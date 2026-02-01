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
	return fmt.Errorf("split not implemented yet")
}
