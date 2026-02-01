package cmd

import (
	"flag"
	"fmt"
	"path/filepath"
)

func runSplit(args []string) {
	fs := flag.NewFlagSet("split", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz (QC output)")
	outDir := fs.String("outdir", "splits", "Output directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Marker FASTA directory (used when -input is empty)")
	markers := fs.String("markers", "COI-5P", "Comma-separated markers to process (used when -input is empty)")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	taxonkitIn := fs.String("taxonkit-input", "taxonkit_input.tsv", "Taxonkit TSV with processid/species labels")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	ranks := splitList(*requireRanks)

	if *input == "" {
		markerList := splitList(*markers)
		if len(markerList) == 0 {
			fatalf("input is empty and markers list is empty")
		}
		for _, marker := range markerList {
			markerInput, err := resolveMarkerInput(*markerDir, marker)
			if err != nil {
				fatalf("marker %s: %v", marker, err)
			}
			baseOut := filepath.Join(*outDir, safeTag(marker))
			if err := splitOne(markerInput, baseOut, *taxonkitIn, ranks, *taxdumpDir, *taxidMap); err != nil {
				fatalf("split %s failed: %v", marker, err)
			}
		}
		return
	}

	if err := splitOne(*input, *outDir, *taxonkitIn, ranks, *taxdumpDir, *taxidMap); err != nil {
		fatalf("split failed: %v", err)
	}
}

func splitOne(input, outDir, taxonkitIn string, ranks []string, taxdumpDir, taxidMap string) error {
	if _, err := loadProcessLabelMap(taxonkitIn); err != nil {
		return err
	}
	return fmt.Errorf("split not implemented yet")
}

func loadProcessLabelMap(path string) (map[string]string, error) {
	in, err := openInput(path)
	if err != nil {
		return nil, fmt.Errorf("open taxonkit input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	opts := DefaultOptions()
	headerSeen := false
	idxProcess := -1
	idxSpecies := -1
	labels := make(map[string]string, 1<<20)

	err = ParseTSV(in, opts, func(row Row) error {
		if !headerSeen {
			headerSeen = true
			idxProcess = indexOfBytes(row.Fields, "processid")
			idxSpecies = indexOfBytes(row.Fields, "species")
			if idxProcess < 0 || idxSpecies < 0 {
				return fmt.Errorf("required headers missing in taxonkit input (need processid, species)")
			}
			return nil
		}

		if idxProcess >= len(row.Fields) || idxSpecies >= len(row.Fields) {
			return fmt.Errorf("line %d: expected at least %d fields", row.Line, maxIndex(idxProcess, idxSpecies)+1)
		}

		pid := string(row.Fields[idxProcess])
		if pid == "" {
			return fmt.Errorf("line %d: empty processid", row.Line)
		}
		if isNone(row.Fields[idxSpecies]) || len(row.Fields[idxSpecies]) == 0 {
			return fmt.Errorf("line %d: empty species label for processid %s", row.Line, pid)
		}
		label := string(row.Fields[idxSpecies])
		if prev, ok := labels[pid]; ok && prev != label {
			return fmt.Errorf("line %d: processid %s maps to multiple labels (%s, %s)", row.Line, pid, prev, label)
		}
		labels[pid] = label
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(labels) == 0 {
		return nil, fmt.Errorf("taxonkit input appears empty: %s", path)
	}
	return labels, nil
}
