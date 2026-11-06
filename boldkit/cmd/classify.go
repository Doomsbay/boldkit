package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/classify"
	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/util"
)

func runClassify(args []string) {
	fs := flag.NewFlagSet("classify", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	outDir := fs.String("outdir", "classifier_outputs", "Output directory")
	classifiers := fs.String("classifier", "blast", "Comma-separated classifiers")
	markerDir := fs.String("marker-dir", "marker_fastas", "Marker FASTA directory (used when -input is empty)")
	markers := fs.String("markers", "COI-5P", "Comma-separated markers to process (used when -input is empty)")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
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
	qcOnly := fs.Bool("qc-only", false, "Run QC only (skip classifier formatting)")
	compress := fs.Bool("compress", false, "Compress classifier output directories (.tar.gz)")
	force := fs.Bool("force", false, "Overwrite existing archives")
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

	run := func(input, outDir string) error {
		return classify.Classify(classify.Config{
			Input:        input,
			OutDir:       outDir,
			Classifiers:  classifierList,
			Ranks:        ranks,
			TaxdumpDir:   *taxdumpDir,
			TaxidMapPath: *taxidMap,
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
			QCOnly:         *qcOnly,
			Compress:       *compress,
			Force:          *force,
			FormatProgress: *formatProgress,
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
			baseOut := filepath.Join(*outDir, util.SafeTag(marker))
			if err := run(markerInput, baseOut); err != nil {
				fatalf("classify %s failed: %v", marker, err)
			}
		}
		return
	}

	if err := run(*input, *outDir); err != nil {
		fatalf("classify failed: %v", err)
	}
}

func resolveMarkerInput(markerDir, marker string) (string, error) {
	if markerDir == "" {
		return "", fmt.Errorf("marker-dir is required")
	}
	if marker == "" {
		return "", fmt.Errorf("marker is empty")
	}
	name := util.SanitizeMarkerBytes(nil, []byte(marker))
	entries, err := os.ReadDir(markerDir)
	if err != nil {
		return "", fmt.Errorf("read marker dir: %w", err)
	}
	raw := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := strings.TrimSuffix(entry.Name(), ".gz")
		if !strings.EqualFold(filename, name+".fasta") {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".gz") {
			return filepath.Join(markerDir, entry.Name()), nil
		}
		raw = filepath.Join(markerDir, entry.Name())
	}
	if raw != "" {
		return raw, nil
	}
	return "", fmt.Errorf("marker FASTA not found in %s (looked for %s.fasta or %s.fasta.gz)", markerDir, name, name)
}
