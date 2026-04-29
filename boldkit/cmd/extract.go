package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/util"
)

const writerBufferSize = 1 << 20

func runExtract(args []string) {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD input file (TSV or Parquet)")
	output := fs.String("output", "taxonkit_input.tsv", "Output taxonkit input TSV")
	curateProtocol := fs.String("curate-protocol", extractCurationProtocolNone, "Extraction curation profile (none,bioscan-5m)")
	curateReport := fs.String("curate-report", "", "Optional extraction curation JSON report path")
	curateAudit := fs.String("curate-audit", "", "Optional extraction curation audit TSV path")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	curationCfg := extractCurationConfig{
		Protocol:   *curateProtocol,
		ReportPath: *curateReport,
		AuditPath:  *curateAudit,
	}.normalized()
	if err := curationCfg.validate(); err != nil {
		fatalf("invalid extraction curation config: %v", err)
	}

	if !*force && fileExists(*output) {
		fmt.Fprintf(os.Stderr, "Output exists, skipping: %s\n", *output)
		return
	}

	totalRows := -1
	if *progressOn {
		count, err := RowCount(*input)
		if err != nil {
			fatalf("count rows failed: %v", err)
		}
		totalRows = int(count)
	}

	reportEvery := 0
	if *progressOn {
		reportEvery = 1
	}

	if _, err := buildTaxonkit(*input, *output, reportEvery, totalRows, curationCfg); err != nil {
		fatalf("build failed: %v", err)
	}
}

func buildTaxonkit(inputPath, outputPath string, reportEvery, totalRows int, curationCfg extractCurationConfig) (int, error) {
	curator, err := newExtractCurator(curationCfg, inputPath)
	if err != nil {
		return 0, fmt.Errorf("create curation profile: %w", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("create output: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	writer := bufio.NewWriterSize(out, writerBufferSize)
	defer func() {
		_ = writer.Flush()
	}()

	progress := newProgress(totalRows, reportEvery)

	opts := DefaultOptions()
	opts.Progress = progress
	opts.SkipProgressFirstRow = true

	var rowCount int
	var (
		idxProcess   = -1
		idxBin       = -1
		idxKingdom   = -1
		idxPhylum    = -1
		idxClass     = -1
		idxOrder     = -1
		idxFamily    = -1
		idxSubfamily = -1
		idxTribe     = -1
		idxGenus     = -1
		idxSpecies   = -1
	)

	err = ParseRows(inputPath, opts, func(row Row) error {
		if idxProcess < 0 {
			idxProcess = util.IndexOfBytes(row.Fields, "processid")
			idxBin = util.IndexOfBytes(row.Fields, "bin_uri")
			idxKingdom = util.IndexOfBytes(row.Fields, "kingdom")
			idxPhylum = util.IndexOfBytes(row.Fields, "phylum")
			idxClass = util.IndexOfBytes(row.Fields, "class")
			idxOrder = util.IndexOfBytes(row.Fields, "order")
			idxFamily = util.IndexOfBytes(row.Fields, "family")
			idxSubfamily = util.IndexOfBytes(row.Fields, "subfamily")
			idxTribe = util.IndexOfBytes(row.Fields, "tribe")
			idxGenus = util.IndexOfBytes(row.Fields, "genus")
			idxSpecies = util.IndexOfBytes(row.Fields, "species")
			if idxProcess < 0 || idxBin < 0 || idxKingdom < 0 || idxPhylum < 0 || idxClass < 0 ||
				idxOrder < 0 || idxFamily < 0 || idxGenus < 0 || idxSpecies < 0 {
				return errors.New("required headers missing in input")
			}
			_, err := writer.WriteString("kingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies\tprocessid\n")
			return err
		}

		rowCount++
		fields := row.Fields

		record := extractTaxonRecord{
			ProcessID: string(util.Field(fields, idxProcess)),
			BinURI:    string(util.Field(fields, idxBin)),
			Kingdom:   string(util.Normalize(util.Field(fields, idxKingdom))),
			Phylum:    string(util.Normalize(util.Field(fields, idxPhylum))),
			Class:     string(util.Normalize(util.Field(fields, idxClass))),
			Order:     string(util.Normalize(util.Field(fields, idxOrder))),
			Family:    string(util.Normalize(util.Field(fields, idxFamily))),
			Subfamily: string(util.Normalize(util.Field(fields, idxSubfamily))),
			Tribe:     string(util.Normalize(util.Field(fields, idxTribe))),
			Genus:     string(util.Normalize(util.Field(fields, idxGenus))),
			Species:   string(util.Normalize(util.Field(fields, idxSpecies))),
		}
		if err := curator.Curate(&record); err != nil {
			return fmt.Errorf("line %d curation failed: %w", rowCount+1, err)
		}

		if record.Genus != "" && record.Species == "" {
			suffix := record.BinURI
			if suffix == "" && !curationCfg.enabled() {
				suffix = record.ProcessID
			}
			if suffix != "" {
				record.Species = record.Genus + " sp. " + suffix
			}
		}

		line := strings.Join([]string{
			record.Kingdom, record.Phylum, record.Class, record.Order, record.Family,
			record.Subfamily, record.Tribe, record.Genus, record.Species, record.ProcessID,
		}, "\t")
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("write row: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	progress.finish()
	if err := curator.Close(); err != nil {
		return 0, fmt.Errorf("finalize curation profile: %w", err)
	}
	return rowCount, nil
}
