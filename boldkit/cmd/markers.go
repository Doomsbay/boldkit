package cmd

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/markers"
)

func runMarkers(args []string) {
	fs := flag.NewFlagSet("markers", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD input file (TSV or Parquet)")
	outDir := fs.String("outdir", "marker_fastas", "Output directory for marker FASTAs")
	markersFilter := fs.String("markers", "", "Comma-separated marker codes to build (empty = all, case-insensitive)")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	gzipOut := fs.Bool("gzip", true, "Compress FASTA outputs to .fasta.gz")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parser worker goroutines (<=0 defaults to GOMAXPROCS)")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	selected := markers.ParseSelection(*markersFilter)

	selected, skip, err := markers.SelectMissing(*outDir, selected, *force)
	if err != nil {
		fatalf("check marker outputs: %v", err)
	}
	if skip {
		fmt.Fprintf(os.Stderr, "Marker FASTAs already exist, skipping: %s\n", *outDir)
		return
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

	if err := markers.BuildMarkerFastas(*input, *outDir, *gzipOut, reportEvery, totalRows, *workers, selected); err != nil {
		fatalf("build failed: %v", err)
	}
}
