package cmd

import (
	"flag"

	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/util"
)

func runQC(args []string) {
	fs := flag.NewFlagSet("qc", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	output := fs.String("output", "", "Output FASTA path")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	minLen := fs.Int("min-length", 0, "Minimum cleaned sequence length (0 disables)")
	maxLen := fs.Int("max-length", 0, "Maximum cleaned sequence length (0 disables)")
	maxN := fs.Int("max-n", -1, "Maximum N count allowed (-1 disables)")
	maxAmbig := fs.Int("max-ambig", -1, "Maximum IUPAC ambiguous count allowed (-1 disables)")
	maxInvalid := fs.Int("max-invalid", 0, "Maximum invalid character count allowed")
	gapPolicy := fs.String("gap-policy", qc.GapPolicyDrop, "Gap character handling: drop records containing gaps, or merge across gaps (remove gaps, join flanks)")
	dedupeSeqs := fs.Bool("dedupe", true, "Drop duplicate sequences (cleaned)")
	dedupeIDs := fs.Bool("dedupe-ids", true, "Drop duplicate sequence IDs")
	progressOn := fs.Bool("progress", true, "Show progress bar (approximate)")
	report := fs.String("report", "", "Optional JSON report output path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	if *input == "" || *output == "" {
		fatalf("input and output are required")
	}
	if *minLen < 0 || *maxLen < 0 {
		fatalf("min-length and max-length must be >= 0")
	}
	if *maxN < -1 || *maxAmbig < -1 {
		fatalf("max-n and max-ambig must be >= -1")
	}
	if *maxInvalid < 0 {
		fatalf("max-invalid must be >= 0")
	}
	if *gapPolicy != qc.GapPolicyDrop && *gapPolicy != qc.GapPolicyMerge {
		fatalf("gap-policy must be one of: %s, %s", qc.GapPolicyDrop, qc.GapPolicyMerge)
	}

	cfg := qc.Config{
		MinLen:       *minLen,
		MaxLen:       *maxLen,
		MaxN:         *maxN,
		MaxAmbig:     *maxAmbig,
		MaxInvalid:   *maxInvalid,
		GapPolicy:    *gapPolicy,
		DedupeSeqs:   *dedupeSeqs,
		DedupeIDs:    *dedupeIDs,
		RequireRanks: util.SplitList(*requireRanks),
		TaxdumpDir:   *taxdumpDir,
		TaxidMapPath: *taxidMap,
		OutputPath:   *output,
		ReportPath:   *report,
		Progress:     *progressOn,
	}

	if err := qc.RunQC(*input, cfg); err != nil {
		fatalf("qc failed: %v", err)
	}
}
