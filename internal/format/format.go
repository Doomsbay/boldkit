package format

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	"github.com/Doomsbay/BoldKit/internal/qc"
	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const writerBufferSize = 1 << 20

var supportedClassifiers = []string{"blast", "kraken2", "sintax", "rdp", "idtaxa", "protax"}

func ValidateClassifiers(classifiers []string) error {
	known := make(map[string]struct{}, len(supportedClassifiers))
	for _, c := range supportedClassifiers {
		known[c] = struct{}{}
	}
	selected := 0
	for _, c := range classifiers {
		name := strings.ToLower(strings.TrimSpace(c))
		if name == "" {
			continue
		}
		if _, ok := known[name]; !ok {
			return fmt.Errorf("unsupported classifier %q (supported: %s)", name, strings.Join(supportedClassifiers, ","))
		}
		selected++
	}
	if selected == 0 {
		return fmt.Errorf("classifier must not be empty (supported: %s)", strings.Join(supportedClassifiers, ","))
	}
	return nil
}

type FormatConfig struct {
	Classifiers  []string
	RequireRanks []string
	Input        string
	OutDir       string
	TaxdumpDir   string
	TaxidMapPath string
	ReportPath   string
	Progress     bool
}

type formatStats struct {
	Total        int
	Written      int
	MissingTaxID int
	MissingRanks int
}

type writerHandle struct {
	w *bufio.Writer
	f *os.File
}

type formatWriters struct {
	blastFasta    writerHandle
	blastMap      writerHandle
	krakenFasta   writerHandle
	sintaxFasta   writerHandle
	rdpTrainFasta writerHandle
	rdpTaxonomy   writerHandle
	idtaxaFasta   writerHandle
	idtaxaLineage writerHandle
	protaxFasta   writerHandle
	protaxMap     writerHandle
	protaxCSV     writerHandle
}

func FormatFasta(cfg FormatConfig) error {
	if err := ValidateClassifiers(cfg.Classifiers); err != nil {
		return err
	}

	in, counter, err := boldio.OpenInputWithCounter(cfg.Input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	var bar *boldprogress.ByteProgress
	var lastCount int64
	if cfg.Progress {
		total := boldio.FileSize(cfg.Input)
		bar = boldprogress.NewByte(total, "format (approx)")
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("create outdir: %w", err)
	}

	taxidPath := cfg.TaxidMapPath
	if taxidPath == "" {
		taxidPath = filepath.Join(cfg.TaxdumpDir, "taxid.map")
	}
	taxidMap, err := boldtaxonomy.LoadTaxidMap(taxidPath)
	if err != nil {
		return err
	}

	nodesPath := filepath.Join(cfg.TaxdumpDir, "nodes.dmp")
	namesPath := filepath.Join(cfg.TaxdumpDir, "names.dmp")
	dump, err := boldtaxonomy.LoadTaxDump(nodesPath, namesPath)
	if err != nil {
		return err
	}

	writers, err := openFormatWriters(cfg.OutDir, cfg.Classifiers)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			discardFormatWriters(writers)
		}
	}()

	stats := formatStats{}
	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		stats.Total++
		if rec.ID == "" {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		taxid, ok := taxidMap[rec.ID]
		if !ok {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		lineage := dump.Lineage(taxid)
		if !boldtaxonomy.HasAllRanks(lineage, cfg.RequireRanks) {
			stats.MissingRanks++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}

		names := BuildLineage(lineage, cfg.RequireRanks)
		if len(names) == 0 {
			stats.MissingRanks++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		seq := rec.Seq

		if writers.blastFasta.w != nil {
			if err := boldio.WriteFasta(writers.blastFasta.w, rec.ID, seq); err != nil {
				return err
			}
		}
		if writers.blastMap.w != nil {
			if _, err := writers.blastMap.w.WriteString(rec.ID + "\t" + strconv.Itoa(taxid) + "\n"); err != nil {
				return fmt.Errorf("write blast map: %w", err)
			}
		}
		if writers.krakenFasta.w != nil {
			header := rec.ID + "|kraken:taxid|" + strconv.Itoa(taxid)
			if err := boldio.WriteFasta(writers.krakenFasta.w, header, seq); err != nil {
				return err
			}
		}
		if writers.sintaxFasta.w != nil {
			header := rec.ID + ";tax=" + SintaxLineage(names)
			if err := boldio.WriteFasta(writers.sintaxFasta.w, header, seq); err != nil {
				return err
			}
		}
		if writers.idtaxaFasta.w != nil {
			if err := boldio.WriteFasta(writers.idtaxaFasta.w, rec.ID, seq); err != nil {
				return err
			}
		}
		if writers.idtaxaLineage.w != nil {
			lineageStr := "Root;" + strings.Join(names, ";")
			if _, err := writers.idtaxaLineage.w.WriteString(rec.ID + "\t" + lineageStr + "\n"); err != nil {
				return fmt.Errorf("write idtaxa lineage: %w", err)
			}
		}
		if writers.protaxFasta.w != nil {
			if err := boldio.WriteFasta(writers.protaxFasta.w, rec.ID, seq); err != nil {
				return err
			}
		}
		if writers.protaxMap.w != nil {
			lineageStr := strings.Join(names, ";")
			if _, err := writers.protaxMap.w.WriteString(rec.ID + "\t" + lineageStr + "\n"); err != nil {
				return fmt.Errorf("write protax map: %w", err)
			}
		}

		stats.Written++
		updateByteProgress(bar, counter, &lastCount)
		return nil
	})
	if err != nil {
		return err
	}
	updateByteProgress(bar, counter, &lastCount)
	if bar != nil {
		bar.Finish()
	}

	if writers.rdpTrainFasta.w != nil {
		if err := formatFastaRdp(cfg, taxidMap, dump, writers); err != nil {
			return fmt.Errorf("rdp format: %w", err)
		}
	}

	if writers.protaxCSV.w != nil {
		if err := formatProtaxCSV(cfg, taxidMap, dump, writers); err != nil {
			return fmt.Errorf("protax csv: %w", err)
		}
	}

	if err := commitFormatWriters(writers); err != nil {
		return fmt.Errorf("finalize outputs: %w", err)
	}
	committed = true

	if cfg.ReportPath != "" {
		if err := qc.WriteReport(cfg.ReportPath, qc.Stats{
			Total:        stats.Total,
			Written:      stats.Written,
			MissingTaxID: stats.MissingTaxID,
			MissingRanks: stats.MissingRanks,
		}); err != nil {
			return err
		}
	}
	util.Logf("format: total=%d kept=%d missing-taxid=%d missing-ranks=%d", stats.Total, stats.Written, stats.MissingTaxID, stats.MissingRanks)
	return nil
}

func formatFastaRdp(cfg FormatConfig, taxidMap map[string]int, dump *boldtaxonomy.TaxDump, writers *formatWriters) error {
	tmpFasta, err := os.CreateTemp("", "rdp_seqs_*.fasta")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFasta.Name()
	defer func() {
		_ = tmpFasta.Close()
		_ = os.Remove(tmpPath)
	}()

	tmpWriter := bufio.NewWriterSize(tmpFasta, writerBufferSize)

	builder := boldtaxonomy.NewRDPTaxonomyBuilder(cfg.RequireRanks)
	var seqCount int

	in, err := boldio.OpenInput(cfg.Input)
	if err != nil {
		return fmt.Errorf("open input for rdp: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		if rec.ID == "" {
			return nil
		}
		taxid, ok := taxidMap[rec.ID]
		if !ok {
			return nil
		}
		lineage := dump.Lineage(taxid)
		if !boldtaxonomy.HasAllRanks(lineage, cfg.RequireRanks) {
			return nil
		}

		names := BuildLineage(lineage, cfg.RequireRanks)
		if len(names) == 0 {
			return nil
		}

		resolved := builder.AddLineage(names)
		if len(resolved) == 0 {
			return nil
		}

		lineageStr := builder.GetLineageString(resolved)
		if _, err := tmpWriter.WriteString(rec.ID + "\t" + lineageStr + "\t" + string(rec.Seq) + "\n"); err != nil {
			return fmt.Errorf("write temp: %w", err)
		}
		seqCount++
		return nil
	})
	if err != nil {
		return err
	}
	if err := tmpWriter.Flush(); err != nil {
		return fmt.Errorf("flush temp: %w", err)
	}
	if err := tmpFasta.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if builder.DisambiguatedCount() > 0 {
		util.Logf("rdp: disambiguated %d taxonomy names due to parent conflicts", builder.DisambiguatedCount())
	}

	if err := builder.WriteTaxonomyFile(writers.rdpTaxonomy.w); err != nil {
		return fmt.Errorf("write taxonomy: %w", err)
	}

	tmpIn, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}
	defer func() {
		_ = tmpIn.Close()
	}()

	scanner := bufio.NewScanner(tmpIn)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		header := parts[0] + "\t" + parts[1]
		if err := boldio.WriteFasta(writers.rdpTrainFasta.w, header, []byte(parts[2])); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan temp: %w", err)
	}

	return nil
}

var protaxCSVRanks = []string{"phylum", "class", "order", "family", "genus", "species"}

func formatProtaxCSV(cfg FormatConfig, taxidMap map[string]int, dump *boldtaxonomy.TaxDump, writers *formatWriters) error {
	if _, err := writers.protaxCSV.w.WriteString(strings.Join(protaxCSVRanks, ",") + ",dna_barcode\n"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	dis := boldtaxonomy.NewNameDisambiguator()
	global := make(map[string]string)

	in, err := boldio.OpenInput(cfg.Input)
	if err != nil {
		return fmt.Errorf("open input for protax csv: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		if rec.ID == "" {
			return nil
		}
		taxid, ok := taxidMap[rec.ID]
		if !ok {
			return nil
		}
		lineage := dump.Lineage(taxid)
		if !boldtaxonomy.HasAllRanks(lineage, cfg.RequireRanks) {
			return nil
		}
		if len(BuildLineage(lineage, cfg.RequireRanks)) == 0 {
			return nil
		}

		row := make([]string, len(protaxCSVRanks)+1)
		parent := "Root"
		for i, rank := range protaxCSVRanks {
			raw := SanitizeTaxon(lineage[rank])
			if raw == "" {
				row[i] = ""
				continue
			}
			name := dis.Resolve(raw, rank, parent, parent).Name
			name = claimProtaxName(global, name, rank, parent, dis)
			row[i] = name
			parent = name
		}
		row[len(row)-1] = string(rec.Seq)
		if _, err := writers.protaxCSV.w.WriteString(strings.Join(row, ",") + "\n"); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if cnt := dis.DisambiguatedCount(); cnt > 0 {
		util.Logf("protax: disambiguated %d taxonomy names due to parent conflicts", cnt)
	}
	return nil
}

func claimProtaxName(global map[string]string, name, rank, parent string, dis *boldtaxonomy.NameDisambiguator) string {
	for attempt := 0; ; attempt++ {
		owner, ok := global[name]
		if !ok || owner == rank {
			global[name] = rank
			return name
		}
		if attempt >= 2 {
			return name
		}
		name = dis.Resolve(name+"_"+rank, rank, parent, parent).Name
	}
}

func openFormatWriters(outDir string, classifiers []string) (*formatWriters, error) {
	w := &formatWriters{}
	opened := false
	defer func() {
		if !opened {
			discardFormatWriters(w)
		}
	}()
	needs := make(map[string]struct{})
	for _, c := range classifiers {
		name := strings.ToLower(strings.TrimSpace(c))
		if name == "" {
			continue
		}
		needs[name] = struct{}{}
	}

	openFasta := func(name string) (writerHandle, error) {
		path := filepath.Join(outDir, name)
		f, err := os.Create(path)
		if err != nil {
			return writerHandle{}, fmt.Errorf("create %s: %w", path, err)
		}
		return writerHandle{w: bufio.NewWriterSize(f, writerBufferSize), f: f}, nil
	}

	if _, ok := needs["blast"]; ok {
		bw, err := openFasta("blast.fasta")
		if err != nil {
			return nil, err
		}
		w.blastFasta = bw
		mw, err := openFasta("blast_seqid2taxid.map")
		if err != nil {
			return nil, err
		}
		w.blastMap = mw
	}
	if _, ok := needs["kraken2"]; ok {
		bw, err := openFasta("kraken2.fasta")
		if err != nil {
			return nil, err
		}
		w.krakenFasta = bw
	}
	if _, ok := needs["sintax"]; ok {
		bw, err := openFasta("sintax.fasta")
		if err != nil {
			return nil, err
		}
		w.sintaxFasta = bw
	}
	if _, ok := needs["rdp"]; ok {
		bw, err := openFasta("rdp_train_seqs.fasta")
		if err != nil {
			return nil, err
		}
		w.rdpTrainFasta = bw
		tw, err := openFasta("rdp_taxonomy.txt")
		if err != nil {
			return nil, err
		}
		w.rdpTaxonomy = tw
	}
	if _, ok := needs["idtaxa"]; ok {
		bw, err := openFasta("idtaxa_seqs.fasta")
		if err != nil {
			return nil, err
		}
		w.idtaxaFasta = bw
		tw, err := openFasta("idtaxa_lineage.tsv")
		if err != nil {
			return nil, err
		}
		w.idtaxaLineage = tw
	}
	if _, ok := needs["protax"]; ok {
		bw, err := openFasta("protax_seqs.fasta")
		if err != nil {
			return nil, err
		}
		w.protaxFasta = bw
		tw, err := openFasta("protax_seqid2tax.tsv")
		if err != nil {
			return nil, err
		}
		w.protaxMap = tw
		cw, err := openFasta("protax_refs.csv")
		if err != nil {
			return nil, err
		}
		w.protaxCSV = cw
	}
	opened = true
	return w, nil
}

func commitFormatWriters(w *formatWriters) error {
	handles := []struct {
		name string
		h    writerHandle
	}{
		{"blast.fasta", w.blastFasta},
		{"blast_seqid2taxid.map", w.blastMap},
		{"kraken2.fasta", w.krakenFasta},
		{"sintax.fasta", w.sintaxFasta},
		{"rdp_train_seqs.fasta", w.rdpTrainFasta},
		{"rdp_taxonomy.txt", w.rdpTaxonomy},
		{"idtaxa_seqs.fasta", w.idtaxaFasta},
		{"idtaxa_lineage.tsv", w.idtaxaLineage},
		{"protax_seqs.fasta", w.protaxFasta},
		{"protax_seqid2tax.tsv", w.protaxMap},
		{"protax_refs.csv", w.protaxCSV},
	}
	for _, h := range handles {
		if h.h.w == nil {
			continue
		}
		if err := h.h.w.Flush(); err != nil {
			return fmt.Errorf("flush %s: %w", h.name, err)
		}
		if h.h.f != nil {
			if err := h.h.f.Close(); err != nil {
				return fmt.Errorf("close %s: %w", h.name, err)
			}
		}
	}
	return nil
}

func discardFormatWriters(w *formatWriters) {
	for _, h := range []writerHandle{w.blastFasta, w.blastMap, w.krakenFasta, w.sintaxFasta, w.rdpTrainFasta, w.rdpTaxonomy, w.idtaxaFasta, w.idtaxaLineage, w.protaxFasta, w.protaxMap, w.protaxCSV} {
		if h.f == nil {
			continue
		}
		_ = h.f.Close()
	}
}

func BuildLineage(lineage map[string]string, ranks []string) []string {
	if len(ranks) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		name := lineage[rank]
		if name == "" {
			return nil
		}
		out = append(out, SanitizeTaxon(name))
	}
	return out
}

func SintaxLineage(names []string) string {
	prefixes := []string{"d", "p", "c", "o", "f", "g", "s"}
	parts := make([]string, 0, len(names))
	for i, name := range names {
		if i >= len(prefixes) {
			break
		}
		parts = append(parts, prefixes[i]+":"+name)
	}
	if len(names) > len(prefixes) {
		log.Printf("sintax: dropping %d ranks beyond species for %v", len(names)-len(prefixes), names)
	}
	return strings.Join(parts, ",")
}

func SanitizeTaxon(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func updateByteProgress(bar *boldprogress.ByteProgress, counter *boldio.CountReader, last *int64) {
	if bar == nil || counter == nil || last == nil {
		return
	}
	cur := counter.Count()
	delta := cur - *last
	if delta <= 0 {
		return
	}
	bar.Add(delta)
	*last = cur
}
