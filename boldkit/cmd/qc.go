package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const (
	gapPolicyDrop  = "drop"
	gapPolicyMerge = "merge"
)

type qcConfig struct {
	MinLen       int
	MaxLen       int
	MaxN         int
	MaxAmbig     int
	MaxInvalid   int
	GapPolicy    string
	DedupeSeqs   bool
	DedupeIDs    bool
	RequireRanks []string
	TaxdumpDir   string
	TaxidMapPath string
	OutputPath   string
	ReportPath   string
	Progress     bool
}

type qcStats struct {
	Total          int `json:"total"`
	Written        int `json:"written"`
	MissingTaxID   int `json:"missing_taxid"`
	MissingRanks   int `json:"missing_ranks"`
	TooShort       int `json:"too_short"`
	TooLong        int `json:"too_long"`
	TooManyN       int `json:"too_many_n"`
	TooManyAmbig   int `json:"too_many_ambig"`
	TooManyInvalid int `json:"too_many_invalid"`
	Gapped         int `json:"gapped"`
	DupeSeq        int `json:"duplicate_sequence"`
	DupeID         int `json:"duplicate_id"`
}

func runQC(args []string) {
	fs := flag.NewFlagSet("qc", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	output := fs.String("output", "", "Output FASTA path")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	minLen := fs.Int("min-length", 0, "Minimum cleaned sequence length (0 disables)")
	maxLen := fs.Int("max-length", 0, "Maximum cleaned sequence length (0 disables)")
	maxN := fs.Int("max-n", -1, "Maximum N count allowed (-1 disables)")
	maxAmbig := fs.Int("max-ambig", -1, "Maximum IUPAC ambiguous count allowed (-1 disables)")
	maxInvalid := fs.Int("max-invalid", 0, "Maximum invalid character count allowed")
	gapPolicy := fs.String("gap-policy", gapPolicyDrop, "Gap character handling: drop records containing gaps, or merge across gaps (remove gaps, join flanks)")
	dedupeSeqs := fs.Bool("dedupe", true, "Drop duplicate sequences (cleaned)")
	dedupeIDs := fs.Bool("dedupe-ids", true, "Drop duplicate sequence IDs")
	progressOn := fs.Bool("progress", true, "Show progress bar (approximate)")
	report := fs.String("report", "", "Optional JSON report output path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	if *input == "" || *output == "" {
		fatalf("input and output are required")
	}
	if *minLen < 0 || *maxLen < 0 {
		fatalf("min-length and max-length must be >= 0")
	}
	if *maxN < -1 || *maxAmbig < -1 {
		fatalf("max-n and max-ambig must be >= -1")
	}
	if *maxInvalid < 0 {
		fatalf("max-invalid must be >= 0")
	}
	if *gapPolicy != gapPolicyDrop && *gapPolicy != gapPolicyMerge {
		fatalf("gap-policy must be one of: %s, %s", gapPolicyDrop, gapPolicyMerge)
	}

	cfg := qcConfig{
		MinLen:       *minLen,
		MaxLen:       *maxLen,
		MaxN:         *maxN,
		MaxAmbig:     *maxAmbig,
		MaxInvalid:   *maxInvalid,
		GapPolicy:    *gapPolicy,
		DedupeSeqs:   *dedupeSeqs,
		DedupeIDs:    *dedupeIDs,
		RequireRanks: util.SplitList(*requireRanks),
		TaxdumpDir:   *taxdumpDir,
		TaxidMapPath: *taxidMap,
		OutputPath:   *output,
		ReportPath:   *report,
		Progress:     *progressOn,
	}

	if err := qcFasta(*input, cfg); err != nil {
		fatalf("qc failed: %v", err)
	}
}

func qcFasta(input string, cfg qcConfig) error {
	in, counter, err := boldio.OpenInputWithCounter(input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	var bar *boldprogress.ByteProgress
	var lastCount int64
	if cfg.Progress {
		total := boldio.FileSize(input)
		bar = boldprogress.NewByte(total, "qc (approx)")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	out, err := os.Create(cfg.OutputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()
	writer := bufio.NewWriterSize(out, writerBufferSize)
	defer func() {
		_ = writer.Flush()
	}()

	var taxidMap map[string]int
	var dump *boldtaxonomy.TaxDump
	if len(cfg.RequireRanks) > 0 || cfg.TaxidMapPath != "" {
		taxidPath := cfg.TaxidMapPath
		if taxidPath == "" {
			taxidPath = filepath.Join(cfg.TaxdumpDir, "taxid.map")
		}
		taxidMap, err = boldtaxonomy.LoadTaxidMap(taxidPath)
		if err != nil {
			return err
		}
	}
	if len(cfg.RequireRanks) > 0 {
		nodesPath := filepath.Join(cfg.TaxdumpDir, "nodes.dmp")
		namesPath := filepath.Join(cfg.TaxdumpDir, "names.dmp")
		dump, err = boldtaxonomy.LoadTaxDump(nodesPath, namesPath)
		if err != nil {
			return err
		}
	}

	stats := qcStats{}
	seenSeqs := make(map[string]struct{})
	seenIDs := make(map[string]struct{})

	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		stats.Total++
		if rec.ID == "" {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.DedupeIDs {
			if _, ok := seenIDs[rec.ID]; ok {
				stats.DupeID++
				updateByteProgress(bar, counter, &lastCount)
				return nil
			}
			seenIDs[rec.ID] = struct{}{}
		}

		var taxid int
		if taxidMap != nil {
			var ok bool
			taxid, ok = taxidMap[rec.ID]
			if !ok {
				stats.MissingTaxID++
				updateByteProgress(bar, counter, &lastCount)
				return nil
			}
		}

		if len(cfg.RequireRanks) > 0 && dump != nil {
			lineage := dump.Lineage(taxid)
			if !boldtaxonomy.HasAllRanks(lineage, cfg.RequireRanks) {
				stats.MissingRanks++
				updateByteProgress(bar, counter, &lastCount)
				return nil
			}
		}

		clean, counts := cleanSequence(rec.Seq)
		if cfg.GapPolicy == gapPolicyDrop && counts.gapped > 0 {
			stats.Gapped++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if len(clean) == 0 {
			stats.TooShort++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.MinLen > 0 && len(clean) < cfg.MinLen {
			stats.TooShort++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.MaxLen > 0 && len(clean) > cfg.MaxLen {
			stats.TooLong++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.MaxN >= 0 && counts.n > cfg.MaxN {
			stats.TooManyN++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.MaxAmbig >= 0 && counts.ambig > cfg.MaxAmbig {
			stats.TooManyAmbig++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if counts.invalid > cfg.MaxInvalid {
			stats.TooManyInvalid++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.DedupeSeqs {
			key := string(clean)
			if _, ok := seenSeqs[key]; ok {
				stats.DupeSeq++
				updateByteProgress(bar, counter, &lastCount)
				return nil
			}
			seenSeqs[key] = struct{}{}
		}

		if _, err := writer.WriteString(">" + rec.ID + "\n"); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
		if _, err := writer.Write(clean); err != nil {
			return fmt.Errorf("write seq: %w", err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("write newline: %w", err)
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
		if err := writeQCReport(cfg.ReportPath, stats); err != nil {
			return err
		}
	}
	util.Logf("qc: total=%d kept=%d drop taxid=%d ranks=%d short=%d long=%d n=%d ambig=%d invalid=%d gaps=%d dup-seq=%d dup-id=%d",
		stats.Total, stats.Written, stats.MissingTaxID, stats.MissingRanks, stats.TooShort, stats.TooLong, stats.TooManyN, stats.TooManyAmbig, stats.TooManyInvalid, stats.Gapped, stats.DupeSeq, stats.DupeID)
	return nil
}

type seqCounts struct {
	n       int
	ambig   int
	invalid int
	gapped  int
}

func cleanSequence(seq []byte) ([]byte, seqCounts) {
	clean := make([]byte, 0, len(seq))
	counts := seqCounts{}
	for _, c := range seq {
		switch c {
		case 'A', 'C', 'G', 'T':
			clean = append(clean, c)
		case 'a', 'c', 'g', 't':
			clean = append(clean, c-32)
		case 'N', 'n':
			counts.n++
		case 'R', 'Y', 'S', 'W', 'K', 'M', 'B', 'D', 'H', 'V',
			'r', 'y', 's', 'w', 'k', 'm', 'b', 'd', 'h', 'v':
			counts.ambig++
		case '-':
			counts.gapped++
		default:
			if c == '\r' || c == '\n' || c == '\t' || c == ' ' {
				continue
			}
			counts.invalid++
		}
	}
	return clean, counts
}

func writeQCReport(path string, stats qcStats) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
