package extract

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const writerBufferSize = 1 << 20

func BuildTaxonkit(inputPath, outputPath string, reportEvery, totalRows int, curationCfg CurationConfig) (int, error) {
	curator, err := NewCurator(curationCfg, inputPath)
	if err != nil {
		return 0, fmt.Errorf("create curation profile: %w", err)
	}
	if c, ok := curator.(*Bioscan5MCurator); ok {
		defer func() { _ = c.closeAudit() }()
	}

	tmpPath := outputPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("create output: %w", err)
	}
	writer := bufio.NewWriterSize(out, writerBufferSize)
	committed := false
	defer func() {
		if !committed {
			_ = out.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	progress := boldprogress.New(totalRows, reportEvery)

	opts := boldio.DefaultOptions()
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

	err = boldio.ParseRows(inputPath, opts, func(row boldio.Row) error {
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

		record := TaxonRecord{
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

	progress.Finish()
	if err := curator.Close(); err != nil {
		return 0, fmt.Errorf("finalize curation profile: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return 0, fmt.Errorf("flush output: %w", err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return 0, fmt.Errorf("publish output: %w", err)
	}
	committed = true
	return rowCount, nil
}
