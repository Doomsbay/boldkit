package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const writerBufferSize = 1 << 20

func runExtract(args []string) {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD TSV input")
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
		count, err := countLines(*input)
		if err != nil {
			fatalf("count rows failed: %v", err)
		}
		if count > 0 {
			totalRows = count - 1
		}
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
	in, err := openInput(inputPath)
	if err != nil {
		return 0, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()
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

	scanner := bufio.NewScanner(in)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("read header: %w", err)
		}
		return 0, errors.New("input TSV is empty")
	}

	header := strings.Split(scanner.Text(), "\t")
	idxProcess := indexOf(header, "processid")
	idxBin := indexOf(header, "bin_uri")
	idxKingdom := indexOf(header, "kingdom")
	idxPhylum := indexOf(header, "phylum")
	idxClass := indexOf(header, "class")
	idxOrder := indexOf(header, "order")
	idxFamily := indexOf(header, "family")
	idxSubfamily := indexOf(header, "subfamily")
	idxTribe := indexOf(header, "tribe")
	idxGenus := indexOf(header, "genus")
	idxSpecies := indexOf(header, "species")
	if idxProcess < 0 || idxBin < 0 || idxKingdom < 0 || idxPhylum < 0 || idxClass < 0 ||
		idxOrder < 0 || idxFamily < 0 || idxSubfamily < 0 || idxTribe < 0 || idxGenus < 0 ||
		idxSpecies < 0 {
		return 0, errors.New("required headers missing in input TSV")
	}

	if _, err := writer.WriteString("kingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies\tprocessid\n"); err != nil {
		return 0, fmt.Errorf("write header: %w", err)
	}

	progress := newProgress(totalRows, reportEvery)
	var rowCount int
	for scanner.Scan() {
		rowCount++
		fields := strings.Split(scanner.Text(), "\t")

		record := extractTaxonRecord{
			ProcessID: field(fields, idxProcess),
			BinURI:    field(fields, idxBin),
			Kingdom:   normalize(field(fields, idxKingdom)),
			Phylum:    normalize(field(fields, idxPhylum)),
			Class:     normalize(field(fields, idxClass)),
			Order:     normalize(field(fields, idxOrder)),
			Family:    normalize(field(fields, idxFamily)),
			Subfamily: normalize(field(fields, idxSubfamily)),
			Tribe:     normalize(field(fields, idxTribe)),
			Genus:     normalize(field(fields, idxGenus)),
			Species:   normalize(field(fields, idxSpecies)),
		}
		if err := curator.Curate(&record); err != nil {
			return 0, fmt.Errorf("line %d curation failed: %w", rowCount+1, err)
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
			record.Kingdom, record.Phylum, record.Class, record.Order, record.Family, record.Subfamily, record.Tribe, record.Genus, record.Species, record.ProcessID,
		}, "\t")
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return 0, fmt.Errorf("write row: %w", err)
		}

		progress.increment()
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan input: %w", err)
	}

	progress.finish()
	if err := curator.Close(); err != nil {
		return 0, fmt.Errorf("finalize curation profile: %w", err)
	}
	return rowCount, nil
}
