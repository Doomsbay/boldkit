package cmd

import (
	"fmt"
	"os"
)

func Execute(args []string) {
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "extract":
		runExtract(args[1:])
	case "markers":
		runMarkers(args[1:])
	case "package":
		runPackage(args[1:])
	case "pipeline":
		runPipeline(args[1:])
	case "classify":
		runClassify(args[1:])
	case "split":
		runSplit(args[1:])
	case "qc":
		runQC(args[1:])
	case "format":
		runFormat(args[1:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "BoldKit - BOLD TSV processing tools")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  boldkit <command> [options]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  extract    Build taxonkit_input.tsv")
	fmt.Fprintln(os.Stderr, "  markers    Build per-marker FASTA files")
	fmt.Fprintln(os.Stderr, "  package    Package release artifacts")
	fmt.Fprintln(os.Stderr, "  pipeline   Full pipeline: extract -> taxdump -> markers -> package (optional)")
	fmt.Fprintln(os.Stderr, "  classify   QC + classifier formatting pipeline")
	fmt.Fprintln(os.Stderr, "  split      QC + open/closed-world split + taxdump prune")
	fmt.Fprintln(os.Stderr, "  qc         QC filter a FASTA against length/ambiguity/taxonomy rules")
	fmt.Fprintln(os.Stderr, "  format     Generate classifier-specific FASTA/map outputs")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Run 'boldkit <command> -h' for command-specific options.")
}
