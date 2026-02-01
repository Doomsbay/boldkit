package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type formatConfig struct {
	Classifiers []string
	RequireRanks []string
	Input        string
	OutDir       string
	TaxdumpDir   string
	TaxidMapPath string
	ReportPath   string
	Progress     bool
}

type formatStats struct {
	Total        int `json:"total"`
	Written      int `json:"written"`
	MissingTaxID int `json:"missing_taxid"`
	MissingRanks int `json:"missing_ranks"`
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
		Classifiers: splitList(*classifiers),
		RequireRanks: splitList(*requireRanks),
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
	rdpFasta      writerHandle
	rdpLineage    writerHandle
	idtaxaFasta   writerHandle
	idtaxaLineage writerHandle
	protaxFasta   writerHandle
	protaxMap     writerHandle
}

func formatFasta(cfg formatConfig) error {
	in, counter, err := openInputWithCounter(cfg.Input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	var bar *byteProgress
	var lastCount int64
	if cfg.Progress {
		total := fileSize(cfg.Input)
		bar = newByteProgress(total, "format (approx)")
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("create outdir: %w", err)
	}

	taxidPath := cfg.TaxidMapPath
	if taxidPath == "" {
		taxidPath = filepath.Join(cfg.TaxdumpDir, "taxid.map")
	}
	taxidMap, err := loadTaxidMap(taxidPath)
	if err != nil {
		return err
	}

	nodesPath := filepath.Join(cfg.TaxdumpDir, "nodes.dmp")
	namesPath := filepath.Join(cfg.TaxdumpDir, "names.dmp")
	dump, err := loadTaxDump(nodesPath, namesPath)
	if err != nil {
		return err
	}

	writers, err := openFormatWriters(cfg.OutDir, cfg.Classifiers)
	if err != nil {
		return err
	}
	defer closeFormatWriters(writers)

	stats := formatStats{}
	err = parseFasta(in, func(rec fastaRecord) error {
		stats.Total++
		if rec.id == "" {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		taxid, ok := taxidMap[rec.id]
		if !ok {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		lineage := dump.lineage(taxid)
		if !hasAllRanks(lineage, cfg.RequireRanks) {
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
		seq := rec.seq

		if writers.blastFasta.w != nil {
			if err := writeFasta(writers.blastFasta.w, rec.id, seq); err != nil {
				return err
			}
		}
		if writers.blastMap.w != nil {
			if _, err := writers.blastMap.w.WriteString(rec.id + "\t" + strconv.Itoa(taxid) + "\n"); err != nil {
				return fmt.Errorf("write blast map: %w", err)
			}
		}
		if writers.krakenFasta.w != nil {
			header := rec.id + "|kraken:taxid|" + strconv.Itoa(taxid)
			if err := writeFasta(writers.krakenFasta.w, header, seq); err != nil {
				return err
			}
		}
		if writers.sintaxFasta.w != nil {
			header := rec.id + ";tax=" + sintaxLineage(names)
			if err := writeFasta(writers.sintaxFasta.w, header, seq); err != nil {
				return err
			}
		}
		if writers.rdpFasta.w != nil {
			if err := writeFasta(writers.rdpFasta.w, rec.id, seq); err != nil {
				return err
			}
		}
		if writers.rdpLineage.w != nil {
			if _, err := writers.rdpLineage.w.WriteString(rec.id + "\t" + strings.Join(names, "\t") + "\n"); err != nil {
				return fmt.Errorf("write rdp lineage: %w", err)
			}
		}
		if writers.idtaxaFasta.w != nil {
			if err := writeFasta(writers.idtaxaFasta.w, rec.id, seq); err != nil {
				return err
			}
		}
		if writers.idtaxaLineage.w != nil {
			lineageStr := "Root;" + strings.Join(names, ";")
			if _, err := writers.idtaxaLineage.w.WriteString(rec.id + "\t" + lineageStr + "\n"); err != nil {
				return fmt.Errorf("write idtaxa lineage: %w", err)
			}
		}
		if writers.protaxFasta.w != nil {
			if err := writeFasta(writers.protaxFasta.w, rec.id, seq); err != nil {
				return err
			}
		}
		if writers.protaxMap.w != nil {
			lineageStr := strings.Join(names, ";")
			if _, err := writers.protaxMap.w.WriteString(rec.id + "\t" + lineageStr + "\n"); err != nil {
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
	logf("format: total=%d kept=%d missing-taxid=%d missing-ranks=%d", stats.Total, stats.Written, stats.MissingTaxID, stats.MissingRanks)
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

	openText := func(name string) (writerHandle, error) {
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
		mw, err := openText("blast_seqid2taxid.map")
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
		bw, err := openFasta("rdp_seqs.fasta")
		if err != nil {
			return nil, err
		}
		tw, err := openText("rdp_lineage.tsv")
		if err != nil {
			return nil, err
		}
		w.rdpFasta = bw
		w.rdpLineage = tw
	}
	if _, ok := needs["idtaxa"]; ok {
		bw, err := openFasta("idtaxa_seqs.fasta")
		if err != nil {
			return nil, err
		}
		tw, err := openText("idtaxa_lineage.tsv")
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
		tw, err := openText("protax_seqid2tax.tsv")
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
	flush(w.rdpFasta)
	flush(w.rdpLineage)
	flush(w.idtaxaFasta)
	flush(w.idtaxaLineage)
	flush(w.protaxFasta)
	flush(w.protaxMap)
}

func writeFasta(w *bufio.Writer, header string, seq []byte) error {
	if _, err := w.WriteString(">" + header + "\n"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(seq); err != nil {
		return fmt.Errorf("write seq: %w", err)
	}
	if _, err := w.WriteString("\n"); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	return nil
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

func buildLineageRaw(lineage map[string]string, ranks []string) []string {
	if len(ranks) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		name := lineage[rank]
		if name == "" {
			return nil
		}
		out = append(out, name)
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
	return strings.Join(parts, ",")
}
