package cmd

import (
	"flag"

	"github.com/Doomsbay/BoldKit/internal/format"
	"github.com/Doomsbay/BoldKit/internal/util"
)

func runFormat(args []string) {
	fs := flag.NewFlagSet("format", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	outDir := fs.String("outdir", "formatted", "Output directory")
	classifiers := fs.String("classifier", "blast,kraken2,sintax", "Comma-separated classifiers (blast,kraken2,sintax,rdp,idtaxa,protax,dnasketch)")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	progressOn := fs.Bool("progress", true, "Show progress bar (approximate)")
	report := fs.String("report", "", "Optional JSON report output path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	if *input == "" {
		fatalf("input is required")
	}
	cfg := format.FormatConfig{
		Classifiers:  util.SplitList(*classifiers),
		RequireRanks: util.SplitList(*requireRanks),
		Input:        *input,
		OutDir:       *outDir,
		TaxdumpDir:   *taxdumpDir,
		TaxidMapPath: *taxidMap,
		ReportPath:   *report,
		Progress:     *progressOn,
	}
	if len(cfg.Classifiers) == 0 {
		fatalf("classifier must not be empty")
	}
	if err := format.FormatFasta(cfg); err != nil {
		fatalf("format failed: %v", err)
	}
}
