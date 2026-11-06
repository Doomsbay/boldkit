package cmd

import (
	"flag"
	"runtime"

	"github.com/Doomsbay/BoldKit/internal/extract"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/markers"
	"github.com/Doomsbay/BoldKit/internal/pipeline"
)

func runPipeline(args []string) {
	fs := flag.NewFlagSet("pipeline", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD input file (TSV or Parquet)")
	taxonkitOut := fs.String("taxonkit-output", "taxonkit_input.tsv", "Output taxonkit input TSV")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Output taxdump directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Output marker FASTA directory")
	releaseDir := fs.String("releases-dir", "releases", "Release artifacts directory")
	taxonkitBin := fs.String("taxonkit-bin", "", "Path to taxonkit binary (default: search PATH)")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	noGzip := fs.Bool("no-gzip", false, "Disable gzip for marker FASTAs")
	markersFilter := fs.String("markers", "", "Comma-separated marker codes to build (empty = all, case-insensitive)")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parser worker goroutines (<=0 defaults to GOMAXPROCS)")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	packageFlag := fs.Bool("package", false, "Create release zips, manifest, and checksums")
	skipManifest := fs.Bool("skip-manifest", false, "Skip manifest.json (only when --package)")
	skipChecksums := fs.Bool("skip-checksums", false, "Skip SHA256SUMS.txt (only when --package)")
	snapshot := fs.String("snapshot-id", "", "Snapshot ID suffix for releases (default: derive from input filename)")
	extractCurateProtocol := fs.String("extract-curate-protocol", extract.ProtocolNone, "Extraction curation profile (none,bioscan-5m)")
	extractCurateReport := fs.String("extract-curate-report", "", "Optional extraction curation JSON report path")
	extractCurateAudit := fs.String("extract-curate-audit", "", "Optional extraction curation audit TSV path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	extractCfg := extract.CurationConfig{
		Protocol:   *extractCurateProtocol,
		ReportPath: *extractCurateReport,
		AuditPath:  *extractCurateAudit,
	}.Normalized()
	if err := extractCfg.Validate(); err != nil {
		fatalf("invalid extraction curation config: %v", err)
	}

	snap := *snapshot
	if snap == "" {
		snap = snapshotID(*input)
	}

	totalRows := -1
	if *progressOn {
		count, err := boldio.RowCount(*input)
		if err != nil {
			fatalf("count rows failed: %v", err)
		}
		totalRows = int(count)
	}

	reportEvery := 0
	if *progressOn {
		reportEvery = 1
	}

	if err := pipeline.Run(pipeline.Config{
		Input:         *input,
		TaxonkitOut:   *taxonkitOut,
		TaxdumpDir:    *taxdumpDir,
		MarkerDir:     *markerDir,
		ReleaseDir:    *releaseDir,
		TaxonkitBin:   *taxonkitBin,
		ReportEvery:   reportEvery,
		TotalRows:     totalRows,
		Workers:       *workers,
		GzipMarkers:   !*noGzip,
		Force:         *force,
		Package:       *packageFlag,
		SkipManifest:  *skipManifest,
		SkipChecksums: *skipChecksums,
		Snapshot:      snap,
		Version:       appVersion,
		Extract:       extractCfg,
		Selected:      markers.ParseSelection(*markersFilter),
	}); err != nil {
		fatalf("pipeline failed: %v", err)
	}
}
