package qc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const (
	GapPolicyDrop  = "drop"
	GapPolicyMerge = "merge"
)

const writerBufferSize = 1 << 20

type Config struct {
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

type Stats struct {
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

func RunQC(input string, cfg Config) error {
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

	if err := rejectAliasedOutput(input, cfg.OutputPath); err != nil {
		return fmt.Errorf("validate output: %w", err)
	}

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

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	out, err := os.Create(cfg.OutputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	writer := bufio.NewWriterSize(out, writerBufferSize)
	committed := false
	defer func() {
		if !committed {
			_ = out.Close()
		}
	}()

	stats := Stats{}
	seenSeqs := make(map[string]struct{})
	seenIDs := make(map[string]struct{})

	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		stats.Total++
		if rec.ID == "" {
			stats.MissingTaxID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
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

		clean, counts := CleanSequence(rec.Seq)
		if cfg.GapPolicy == GapPolicyDrop && counts.Gapped > 0 {
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
		if cfg.MaxN >= 0 && counts.N > cfg.MaxN {
			stats.TooManyN++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.MaxAmbig >= 0 && counts.Ambig > cfg.MaxAmbig {
			stats.TooManyAmbig++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if counts.Invalid > cfg.MaxInvalid {
			stats.TooManyInvalid++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}

		dupSeq := false
		if cfg.DedupeSeqs {
			_, dupSeq = seenSeqs[string(clean)]
		}
		dupID := false
		if cfg.DedupeIDs {
			_, dupID = seenIDs[rec.ID]
		}
		if dupSeq {
			stats.DupeSeq++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if dupID {
			stats.DupeID++
			updateByteProgress(bar, counter, &lastCount)
			return nil
		}
		if cfg.DedupeSeqs {
			seenSeqs[string(clean)] = struct{}{}
		}
		if cfg.DedupeIDs {
			seenIDs[rec.ID] = struct{}{}
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

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	committed = true

	if cfg.ReportPath != "" {
		if err := WriteReport(cfg.ReportPath, stats); err != nil {
			return err
		}
	}
	util.Logf("qc: total=%d kept=%d drop taxid=%d ranks=%d short=%d long=%d n=%d ambig=%d invalid=%d gaps=%d dup-seq=%d dup-id=%d",
		stats.Total, stats.Written, stats.MissingTaxID, stats.MissingRanks, stats.TooShort, stats.TooLong, stats.TooManyN, stats.TooManyAmbig, stats.TooManyInvalid, stats.Gapped, stats.DupeSeq, stats.DupeID)
	return nil
}

type SeqCounts struct {
	N       int
	Ambig   int
	Invalid int
	Gapped  int
}

func rejectAliasedOutput(input, output string) error {
	same := filepath.Clean(input) == filepath.Clean(output)
	if !same {
		inAbs, err := filepath.Abs(input)
		if err != nil {
			return fmt.Errorf("resolve input path: %w", err)
		}
		outAbs, err := filepath.Abs(output)
		if err != nil {
			return fmt.Errorf("resolve output path: %w", err)
		}
		same = inAbs == outAbs
	}
	if !same {
		inInfo, err := os.Stat(input)
		if err != nil {
			return fmt.Errorf("stat input: %w", err)
		}
		outInfo, err := os.Stat(output)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("stat output: %w", err)
			}
		} else {
			same = os.SameFile(inInfo, outInfo)
		}
	}
	if same {
		return fmt.Errorf("output %q refers to the input FASTA", output)
	}
	return nil
}

func CleanSequence(seq []byte) ([]byte, SeqCounts) {
	clean := make([]byte, 0, len(seq))
	counts := SeqCounts{}
	for _, c := range seq {
		switch c {
		case 'A', 'C', 'G', 'T':
			clean = append(clean, c)
		case 'a', 'c', 'g', 't':
			clean = append(clean, c-32)
		case 'N', 'n':
			counts.N++
		case 'R', 'Y', 'S', 'W', 'K', 'M', 'B', 'D', 'H', 'V',
			'r', 'y', 's', 'w', 'k', 'm', 'b', 'd', 'h', 'v':
			counts.Ambig++
		case '-':
			counts.Gapped++
		default:
			if c == '\r' || c == '\n' || c == '\t' || c == ' ' {
				continue
			}
			counts.Invalid++
		}
	}
	return clean, counts
}

func WriteReport(path string, stats Stats) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		_ = f.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
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
