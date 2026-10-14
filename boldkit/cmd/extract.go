package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/Doomsbay/BoldKit/internal/extract"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
)

func runExtract(args []string) {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD input file (TSV or Parquet)")
	output := fs.String("output", "taxonkit_input.tsv", "Output taxonkit input TSV")
	curateProtocol := fs.String("curate-protocol", extract.ProtocolNone, "Extraction curation profile (none,bioscan-5m)")
	curateReport := fs.String("curate-report", "", "Optional extraction curation JSON report path")
	curateAudit := fs.String("curate-audit", "", "Optional extraction curation audit TSV path")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	curationCfg := extract.CurationConfig{
		Protocol:   *curateProtocol,
		ReportPath: *curateReport,
		AuditPath:  *curateAudit,
	}.Normalized()
	if err := curationCfg.Validate(); err != nil {
		fatalf("invalid extraction curation config: %v", err)
	}

	if !*force && boldio.FileExists(*output) {
		fmt.Fprintf(os.Stderr, "Output exists, skipping: %s\n", *output)
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

	if _, err := extract.BuildTaxonkit(*input, *output, reportEvery, totalRows, curationCfg); err != nil {
		fatalf("build failed: %v", err)
	}
}
