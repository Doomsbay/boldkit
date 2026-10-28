package cmd

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/format"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
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
			if err := classifyOne(markerInput, baseOut, classifierList, ranks, *taxdumpDir, *taxidMap, *qcMin, *qcMax, *qcMaxN, *qcMaxAmbig, *qcMaxInvalid, *qcGapPolicy, *qcDedupe, *qcDedupeIDs, *qcProgress, *formatProgress, *qcOnly, *compress, *force); err != nil {
				fatalf("classify %s failed: %v", marker, err)
			}
		}
		return
	}

	if err := classifyOne(*input, *outDir, classifierList, ranks, *taxdumpDir, *taxidMap, *qcMin, *qcMax, *qcMaxN, *qcMaxAmbig, *qcMaxInvalid, *qcGapPolicy, *qcDedupe, *qcDedupeIDs, *qcProgress, *formatProgress, *qcOnly, *compress, *force); err != nil {
		fatalf("classify failed: %v", err)
	}
}

func classifyOne(input, outDir string, classifierList, ranks []string, taxdumpDir, taxidMap string, qcMin, qcMax, qcMaxN, qcMaxAmbig, qcMaxInvalid int, qcGapPolicy string, qcDedupe, qcDedupeIDs, qcProgress, formatProgress, qcOnly, compress, force bool) error {
	base := qcBaseName(input)
	qcOut := filepath.Join(outDir, "qc", base+".fasta")
	qcCfg := qc.Config{
		MinLen:       qcMin,
		MaxLen:       qcMax,
		MaxN:         qcMaxN,
		MaxAmbig:     qcMaxAmbig,
		MaxInvalid:   qcMaxInvalid,
		GapPolicy:    qcGapPolicy,
		DedupeSeqs:   qcDedupe,
		DedupeIDs:    qcDedupeIDs,
		RequireRanks: ranks,
		TaxdumpDir:   taxdumpDir,
		TaxidMapPath: taxidMap,
		OutputPath:   qcOut,
		Progress:     qcProgress,
	}

	util.Logf("QC -> %s", qcOut)
	if err := qc.RunQC(input, qcCfg); err != nil {
		return fmt.Errorf("qc failed: %w", err)
	}

	if qcOnly {
		return nil
	}

	for _, classifier := range classifierList {
		if classifier == "" {
			continue
		}
		name := strings.ToLower(classifier)
		outPath := filepath.Join(outDir, name)
		cfg := format.FormatConfig{
			Classifiers:  []string{name},
			RequireRanks: ranks,
			Input:        qcOut,
			OutDir:       outPath,
			TaxdumpDir:   taxdumpDir,
			TaxidMapPath: taxidMap,
			Progress:     formatProgress,
		}
		util.Logf("Format %s -> %s", name, outPath)
		if err := format.FormatFasta(cfg); err != nil {
			return fmt.Errorf("format %s failed: %w", name, err)
		}

		if compress {
			archive := filepath.Join(outDir, name+".tar.gz")
			if err := boldio.PackageDirGzip(outPath, archive, force); err != nil {
				return fmt.Errorf("compress %s failed: %w", name, err)
			}
		}
	}
	return nil
}

func qcBaseName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".gz")
	ext := filepath.Ext(base)
	base = strings.TrimSuffix(base, ext)
	if base == "" {
		return "qc_output"
	}
	return base
}

func resolveMarkerInput(markerDir, marker string) (string, error) {
	if markerDir == "" {
		return "", fmt.Errorf("marker-dir is required")
	}
	if marker == "" {
		return "", fmt.Errorf("marker is empty")
	}
	gz := filepath.Join(markerDir, marker+".fasta.gz")
	if boldio.FileExists(gz) {
		return gz, nil
	}
	raw := filepath.Join(markerDir, marker+".fasta")
	if boldio.FileExists(raw) {
		return raw, nil
	}
	return "", fmt.Errorf("marker FASTA not found (%s or %s)", gz, raw)
}
