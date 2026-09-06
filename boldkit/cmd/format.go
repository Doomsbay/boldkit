package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type formatConfig struct {
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

func runFormat(args []string) {
	fs := flag.NewFlagSet("format", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	outDir := fs.String("outdir", "formatted", "Output directory")
	classifiers := fs.String("classifier", "blast,kraken2,sintax", "Comma-separated classifiers (blast,kraken2,sintax,rdp,idtaxa,protax,dnasketch)")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	progressOn := fs.Bool("progress", true, "Show progress bar (approximate)")
	report := fs.String("report", "", "Optional JSON report output path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	if *input == "" {
		fatalf("input is required")
	}
	cfg := formatConfig{
		Classifiers:  util.SplitList(*classifiers),
		RequireRanks: util.SplitList(*requireRanks),
		Input:        *input,
		OutDir:       *outDir,
		TaxdumpDir:   *taxdumpDir,
		TaxidMapPath: *taxidMap,
		ReportPath:   *report,
		Progress:     *progressOn,
	}
	if len(cfg.Classifiers) == 0 {
		fatalf("classifier must not be empty")
	}
	if err := formatFasta(cfg); err != nil {
		fatalf("format failed: %v", err)
	}
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
}

func formatFasta(cfg formatConfig) error {
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
	defer closeFormatWriters(writers)

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

		names := buildLineage(lineage, cfg.RequireRanks)
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
			header := rec.ID + ";tax=" + sintaxLineage(names)
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

	if cfg.ReportPath != "" {
		if err := writeQCReport(cfg.ReportPath, qcStats{
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

func formatFastaRdp(cfg formatConfig, taxidMap map[string]int, dump *boldtaxonomy.TaxDump, writers *formatWriters) error {
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

		names := buildLineage(lineage, cfg.RequireRanks)
		if len(names) == 0 {
			return nil
		}

		resolved := builder.AddLineage(names)
		if len(resolved) == 0 {
			return nil
		}

		lineageStr := strings.Join(resolved, "|")
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
	_ = tmpFasta.Close()

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
		seqID := parts[0]
		keys := strings.Split(parts[1], "|")
		seq := parts[2]

		lineageNames := builder.GetLineageString(keys)
		header := seqID + "\t" + lineageNames
		if err := boldio.WriteFasta(writers.rdpTrainFasta.w, header, []byte(seq)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan temp: %w", err)
	}

	return nil
}

func openFormatWriters(outDir string, classifiers []string) (*formatWriters, error) {
	w := &formatWriters{}
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
		mw, err := openFasta("blast_seqid2taxid.map")
		if err != nil {
			return nil, err
		}
		w.blastFasta = bw
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
		tw, err := openFasta("rdp_taxonomy.txt")
		if err != nil {
			return nil, err
		}
		w.rdpTrainFasta = bw
		w.rdpTaxonomy = tw
	}
	if _, ok := needs["idtaxa"]; ok {
		bw, err := openFasta("idtaxa_seqs.fasta")
		if err != nil {
			return nil, err
		}
		tw, err := openFasta("idtaxa_lineage.tsv")
		if err != nil {
			return nil, err
		}
		w.idtaxaFasta = bw
		w.idtaxaLineage = tw
	}
	if _, ok := needs["protax"]; ok {
		bw, err := openFasta("protax_seqs.fasta")
		if err != nil {
			return nil, err
		}
		tw, err := openFasta("protax_seqid2tax.tsv")
		if err != nil {
			return nil, err
		}
		w.protaxFasta = bw
		w.protaxMap = tw
	}
	return w, nil
}

func closeFormatWriters(w *formatWriters) {
	flush := func(h writerHandle) {
		if h.w == nil {
			return
		}
		_ = h.w.Flush()
		if h.f != nil {
			_ = h.f.Close()
		}
	}
	flush(w.blastFasta)
	flush(w.blastMap)
	flush(w.krakenFasta)
	flush(w.sintaxFasta)
	flush(w.rdpTrainFasta)
	flush(w.rdpTaxonomy)
	flush(w.idtaxaFasta)
	flush(w.idtaxaLineage)
	flush(w.protaxFasta)
	flush(w.protaxMap)
}

func buildLineage(lineage map[string]string, ranks []string) []string {
	if len(ranks) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		name := lineage[rank]
		if name == "" {
			return nil
		}
		out = append(out, sanitizeTaxon(name))
	}
	return out
}

func sintaxLineage(names []string) string {
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

func sanitizeTaxon(name string) string {
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
