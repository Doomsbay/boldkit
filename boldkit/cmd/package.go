package cmd

import (
	"flag"

	"github.com/Doomsbay/BoldKit/internal/pipeline"
)

func runPackage(args []string) {
	fs := flag.NewFlagSet("package", flag.ExitOnError)
	taxonkitOut := fs.String("taxonkit-output", "taxonkit_input.tsv", "Input taxonkit TSV to include")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Input taxdump directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Input marker FASTA directory")
	releaseDir := fs.String("releases-dir", "releases", "Release artifacts directory")
	snapshot := fs.String("snapshot-id", "", "Snapshot ID suffix for releases")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	skipManifest := fs.Bool("skip-manifest", false, "Skip manifest.json")
	skipChecksums := fs.Bool("skip-checksums", false, "Skip SHA256SUMS.txt")
	moveInputs := fs.Bool("move", true, "Move inputs into releases dir before packaging")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	snap := *snapshot
	if snap == "" {
		snap = snapshotID(*taxonkitOut)
	}

	if err := pipeline.PackageRelease(pipeline.ReleaseConfig{
		TaxdumpDir:    *taxdumpDir,
		MarkerDir:     *markerDir,
		TaxonkitOut:   *taxonkitOut,
		ReleaseDir:    *releaseDir,
		Snapshot:      snap,
		Version:       appVersion,
		Force:         *force,
		SkipManifest:  *skipManifest,
		SkipChecksums: *skipChecksums,
		MoveInputs:    *moveInputs,
	}); err != nil {
		fatalf("package failed: %v", err)
	}
}
