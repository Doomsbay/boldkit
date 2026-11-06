package split

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/format"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const (
	bucketSeenTrain  = "seen_train"
	bucketSeenVal    = "seen_val"
	bucketSeenTest   = "seen_test"
	bucketUnseenTest = "test_unseen"
	bucketUnseenVal  = "val_unseen"
	bucketUnseenKeys = "keys_unseen"
	bucketHeldout    = "other_heldout"
	bucketPretrain   = "pretrain"
)

type splitStats struct {
	TotalRecords     int `json:"total_records"`
	TotalClasses     int `json:"total_classes"`
	SeenClasses      int `json:"seen_classes"`
	UnseenClasses    int `json:"unseen_classes"`
	HeldoutClasses   int `json:"heldout_classes"`
	SeenTrainRecords int `json:"seen_train_records"`
	SeenValRecords   int `json:"seen_val_records"`
	SeenTestRecords  int `json:"seen_test_records"`
	UnseenTest       int `json:"test_unseen_records"`
	UnseenVal        int `json:"val_unseen_records"`
	UnseenKey        int `json:"keys_unseen_records"`
	HeldoutRecords   int `json:"other_heldout_records"`
	PretrainRecords  int `json:"pretrain_records"`
	ConflictGroups   int `json:"conflict_groups"`
	ConflictRecords  int `json:"conflict_records"`

	ForcedSeenClasses    int `json:"forced_seen_classes"`
	ForcedHeldoutClasses int `json:"forced_heldout_classes"`
	IncludeUnknown       int `json:"include_unknown"`
	HoldoutUnknown       int `json:"holdout_unknown"`
}

type strategyReport struct {
	MinSeenRecords        int     `json:"min_seen_records"`
	MinSeenBarcodes       int     `json:"min_seen_barcodes"`
	MaxTestTargetPerClass int     `json:"max_test_target_per_class"`
	SeenTestTarget        float64 `json:"seen_test_frac"`
	SeenValTarget         float64 `json:"seen_val_frac"`
	UnseenValTarget       float64 `json:"unseen_val_frac"`
	HeldoutFrac           float64 `json:"heldout_frac"`
}

func newStrategyReport(s SplitStrategy) strategyReport {
	return strategyReport{
		MinSeenRecords:        s.MinSeenRecords,
		MinSeenBarcodes:       s.MinSeenBarcodes,
		MaxTestTargetPerClass: s.MaxTestTargetPerClass,
		SeenTestTarget:        s.SeenTestTarget,
		SeenValTarget:         s.SeenValTarget,
		UnseenValTarget:       s.UnseenValTarget,
		HeldoutFrac:           s.HeldoutFrac,
	}
}

type splitReport struct {
	Input       string         `json:"input"`
	OutDir      string         `json:"out_dir"`
	Classifiers []string       `json:"classifiers"`
	PrunedTaxa  int            `json:"pruned_taxids"`
	Strategy    strategyReport `json:"strategy"`
	Stats       splitStats     `json:"stats"`
}

type Config struct {
	Input          string
	OutDir         string
	TaxonkitInput  string
	Ranks          []string
	Classifiers    []string
	TaxdumpDir     string
	TaxidMapPath   string
	QC             qc.Config
	RunQC          bool
	FormatProgress bool
	Strategy       SplitStrategy
}

func SplitOne(cfg Config) error {
	splitInput := cfg.Input
	var (
		conflicts  conflictReport
		labels     map[string]string
		invalidIDs map[string]struct{}
	)
	if cfg.RunQC {
		fastaIDs, err := collectFastaIDs(cfg.Input, cfg.QC.DedupeIDs)
		if err != nil {
			return err
		}
		labels, invalidIDs, err = loadProcessLabelMap(cfg.TaxonkitInput, fastaIDs)
		if err != nil {
			return err
		}
		conflicts, err = detectSequenceLabelConflicts(cfg.Input, labels, invalidIDs, cfg.QC.GapPolicy)
		if err != nil {
			return err
		}
		qcOut := filepath.Join(cfg.OutDir, "qc", boldio.QCBaseName(cfg.Input)+".fasta")
		util.Logf("split: QC -> %s", qcOut)
		qcCfg := cfg.QC
		qcCfg.RequireRanks = cfg.Ranks
		qcCfg.TaxdumpDir = cfg.TaxdumpDir
		qcCfg.TaxidMapPath = cfg.TaxidMapPath
		qcCfg.OutputPath = qcOut
		if err := qc.RunQC(cfg.Input, qcCfg); err != nil {
			return fmt.Errorf("qc failed: %w", err)
		}
		splitInput = qcOut
	} else {
		fastaIDs, err := collectFastaIDs(splitInput, false)
		if err != nil {
			return err
		}
		labels, invalidIDs, err = loadProcessLabelMap(cfg.TaxonkitInput, fastaIDs)
		if err != nil {
			return err
		}
	}

	strategy := cfg.Strategy
	if strategy.isZero() {
		strategy = DefaultStrategy()
	}
	plan, stats, err := buildSplitPlan(splitInput, labels, invalidIDs, conflicts.hashes, strategy)
	if err != nil {
		return err
	}
	if cfg.RunQC {
		stats.ConflictGroups = conflicts.groups
		stats.ConflictRecords = conflicts.records
	}

	writeStats, seenTrainIDs, err := writeSplitFastas(splitInput, cfg.OutDir, plan, labels)
	if err != nil {
		return err
	}
	stats.SeenTrainRecords = writeStats[bucketSeenTrain]
	stats.SeenValRecords = writeStats[bucketSeenVal]
	stats.SeenTestRecords = writeStats[bucketSeenTest]
	stats.UnseenTest = writeStats[bucketUnseenTest]
	stats.UnseenVal = writeStats[bucketUnseenVal]
	stats.UnseenKey = writeStats[bucketUnseenKeys]
	stats.HeldoutRecords = writeStats[bucketHeldout]
	stats.PretrainRecords = writeStats[bucketPretrain]

	prunedDir, keptTaxids, err := pruneTaxdumpForSeenTrain(seenTrainIDs, cfg.TaxdumpDir, cfg.TaxidMapPath, cfg.OutDir)
	if err != nil {
		return err
	}

	seenTrain := filepath.Join(cfg.OutDir, "seen_train.fasta")
	formatOut := filepath.Join(cfg.OutDir, "formatted")
	util.Logf("split: format references from %s -> %s", seenTrain, formatOut)
	if err := format.FormatFasta(format.FormatConfig{
		Classifiers:  cfg.Classifiers,
		RequireRanks: cfg.Ranks,
		Input:        seenTrain,
		OutDir:       formatOut,
		TaxdumpDir:   prunedDir,
		TaxidMapPath: filepath.Join(prunedDir, "taxid.map"),
		Progress:     cfg.FormatProgress,
	}); err != nil {
		return fmt.Errorf("format references: %w", err)
	}

	util.Logf("split: records=%d classes=%d seen-classes=%d unseen-classes=%d heldout-classes=%d", stats.TotalRecords, stats.TotalClasses, stats.SeenClasses, stats.UnseenClasses, stats.HeldoutClasses)
	util.Logf("split: pruned taxdump -> %s (kept_taxids=%d)", prunedDir, keptTaxids)
	reportPath := filepath.Join(cfg.OutDir, "split_report.json")
	if err := writeSplitReport(reportPath, splitReport{
		Input:       splitInput,
		OutDir:      cfg.OutDir,
		Classifiers: cfg.Classifiers,
		PrunedTaxa:  keptTaxids,
		Strategy:    newStrategyReport(strategy),
		Stats:       stats,
	}); err != nil {
		return err
	}
	util.Logf("split: report -> %s", reportPath)
	return nil
}

func collectFastaIDs(input string, allowDuplicates bool) (map[string]struct{}, error) {
	in, err := boldio.OpenInput(input)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	ids := make(map[string]struct{}, 1<<20)
	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		if rec.ID == "" {
			return fmt.Errorf("found FASTA record with empty ID")
		}
		if _, dup := ids[rec.ID]; dup && !allowDuplicates {
			return fmt.Errorf("duplicate processid in input FASTA: %s", rec.ID)
		}
		ids[rec.ID] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("input FASTA appears empty: %s", input)
	}
	return ids, nil
}

func loadProcessLabelMap(path string, wantedIDs map[string]struct{}) (map[string]string, map[string]struct{}, error) {
	in, err := boldio.OpenInput(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open taxonkit input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	opts := boldio.DefaultOptions()
	headerSeen := false
	idxProcess := -1
	idxSpecies := -1
	labels := make(map[string]string, len(wantedIDs))
	invalid := make(map[string]struct{})
	found := 0

	err = boldio.ParseTSV(in, opts, func(row boldio.Row) error {
		if !headerSeen {
			headerSeen = true
			idxProcess = util.IndexOfBytes(row.Fields, "processid")
			idxSpecies = util.IndexOfBytes(row.Fields, "species")
			if idxProcess < 0 || idxSpecies < 0 {
				return fmt.Errorf("required headers missing in taxonkit input (need processid, species)")
			}
			return nil
		}

		if idxProcess >= len(row.Fields) || idxSpecies >= len(row.Fields) {
			return fmt.Errorf("line %d: expected at least %d fields", row.Line, util.MaxIndex(idxProcess, idxSpecies)+1)
		}

		pid := string(row.Fields[idxProcess])
		if pid == "" {
			return fmt.Errorf("line %d: empty processid", row.Line)
		}
		if _, need := wantedIDs[pid]; !need {
			return nil
		}

		species := strings.TrimSpace(string(row.Fields[idxSpecies]))
		if species == "" || util.IsNull([]byte(species)) {
			invalid[pid] = struct{}{}
			return nil
		}
		label := species
		if prev, ok := labels[pid]; ok && prev != label {
			return fmt.Errorf("line %d: processid %s maps to multiple labels (%s, %s)", row.Line, pid, prev, label)
		}
		labels[pid] = label
		found++
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	for pid := range wantedIDs {
		if _, ok := labels[pid]; ok {
			continue
		}
		if _, bad := invalid[pid]; bad {
			continue
		}
		invalid[pid] = struct{}{}
	}
	if found == 0 {
		return nil, nil, fmt.Errorf("taxonkit input has no matching process IDs for input FASTA: %s", path)
	}
	if len(invalid) > 0 {
		util.Logf("split: %d records missing species label (moved to %s)", len(invalid), bucketPretrain)
	}
	return labels, invalid, nil
}

func writeSplitReport(path string, report splitReport) error {
	out, err := boldio.CreateAtomic(path)
	if err != nil {
		return fmt.Errorf("create split report: %w", err)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		out.Abort()
		return fmt.Errorf("write split report: %w", err)
	}
	return out.Commit()
}
