package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/split"
	"github.com/Doomsbay/BoldKit/internal/util"
)

func runSplit(args []string) {
	fs := flag.NewFlagSet("split", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	outDir := fs.String("outdir", "libraries", "Output directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Marker FASTA directory (used when -input is empty)")
	markers := fs.String("markers", "COI-5P", "Comma-separated markers to process (used when -input is empty)")
	classifiers := fs.String("classifier", "blast,kraken2,sintax", "Comma-separated classifiers for final reference formatting")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	taxonkitIn := fs.String("taxonkit-input", "taxonkit_input.tsv", "Taxonkit TSV with processid/species labels")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	strategy := split.DefaultStrategy()
	fs.IntVar(&strategy.MinSeenRecords, "seen-min-records", strategy.MinSeenRecords, "Min records for a species to qualify as seen")
	fs.IntVar(&strategy.MinSeenBarcodes, "seen-min-bins", strategy.MinSeenBarcodes, "Min distinct barcode groups for a species to qualify as seen")
	fs.IntVar(&strategy.MaxTestTargetPerClass, "test-cap", strategy.MaxTestTargetPerClass, "Ceiling on test records per species")
	fs.Float64Var(&strategy.SeenTestTarget, "test-frac", strategy.SeenTestTarget, "Test fraction target for seen classes")
	fs.Float64Var(&strategy.SeenValTarget, "val-frac-seen", strategy.SeenValTarget, "Validation fraction target for seen classes")
	fs.Float64Var(&strategy.UnseenValTarget, "val-frac-unseen", strategy.UnseenValTarget, "Validation fraction target for unseen classes")
	fs.Float64Var(&strategy.HeldoutFrac, "heldout-frac", strategy.HeldoutFrac, "Fraction of unseen classes held out entirely (0 disables the heldout bucket)")
	includeSpecies := fs.String("include-species", "", "File listing species forced into the seen training reference (one name per line)")
	holdoutSpecies := fs.String("holdout-species", "", "File listing species forced into the heldout bucket (one name per line)")
	runQC := fs.Bool("run-qc", true, "Run QC before splitting")
	qcMin := fs.Int("qc-min-length", 200, "QC minimum cleaned length")
	qcMax := fs.Int("qc-max-length", 700, "QC maximum cleaned length")
	qcMaxN := fs.Int("qc-max-n", 0, "QC maximum N count")
	qcMaxAmbig := fs.Int("qc-max-ambig", 0, "QC maximum IUPAC ambiguous count")
	qcMaxInvalid := fs.Int("qc-max-invalid", 0, "QC maximum invalid character count")
	qcGapPolicy := fs.String("qc-gap-policy", qc.GapPolicyDrop, "QC gap handling: drop records containing gaps, merge across gaps (remove gaps, join flanks), or keep the alignment frame (terminal gaps become N, internal gaps preserved)")
	qcDedupe := fs.Bool("qc-dedupe", true, "QC drop duplicate sequences")
	qcDedupeIDs := fs.Bool("qc-dedupe-ids", true, "QC drop duplicate IDs")
	qcProgress := fs.Bool("qc-progress", true, "Show QC progress bar (approximate)")
	formatProgress := fs.Bool("format-progress", true, "Show format progress bar (approximate)")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	ranks := util.SplitList(*requireRanks)
	classifierList := util.SplitList(*classifiers)
	if len(classifierList) == 0 {
		fatalf("classifier must not be empty")
	}
	if err := qc.ValidateGapPolicy(*qcGapPolicy); err != nil {
		fatalf("%v", err)
	}
	if *includeSpecies != "" {
		names, err := readSpeciesList(*includeSpecies)
		if err != nil {
			fatalf("read include species list: %v", err)
		}
		strategy.IncludeSpecies = names
	}
	if *holdoutSpecies != "" {
		names, err := readSpeciesList(*holdoutSpecies)
		if err != nil {
			fatalf("read holdout species list: %v", err)
		}
		strategy.HoldoutSpecies = names
	}

	run := func(input, outDir string) error {
		return split.SplitOne(split.Config{
			Input:          input,
			OutDir:         outDir,
			TaxonkitInput:  *taxonkitIn,
			Ranks:          ranks,
			Classifiers:    classifierList,
			TaxdumpDir:     *taxdumpDir,
			TaxidMapPath:   *taxidMap,
			RunQC:          *runQC,
			FormatProgress: *formatProgress,
			Strategy:       strategy,
			QC: qc.Config{
				MinLen:     *qcMin,
				MaxLen:     *qcMax,
				MaxN:       *qcMaxN,
				MaxAmbig:   *qcMaxAmbig,
				MaxInvalid: *qcMaxInvalid,
				GapPolicy:  *qcGapPolicy,
				DedupeSeqs: *qcDedupe,
				DedupeIDs:  *qcDedupeIDs,
				Progress:   *qcProgress,
			},
		})
	}

	if *input == "" {
		markerList := util.SplitList(*markers)
		if len(markerList) == 0 {
			fatalf("input is empty and markers list is empty")
		}
		for _, marker := range markerList {
			markerInput, err := resolveMarkerInput(*markerDir, marker)
			if err != nil {
				fatalf("marker %s: %v", marker, err)
			}
			baseOut := filepath.Join(*outDir, safeTag(marker))
			if err := run(markerInput, baseOut); err != nil {
				fatalf("split %s failed: %v", marker, err)
			}
		}
		return
	}

	if err := run(*input, *outDir); err != nil {
		fatalf("split failed: %v", err)
	}
}

func readSpeciesList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}
