package split

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Doomsbay/BoldKit/internal/qc"
	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
)

func splitTestGammaSequences() []string {
	seqs := make([]string, 8)
	for i := 0; i < 8; i++ {
		b := make([]byte, 8)
		for j := 0; j < 8; j++ {
			if (i>>j)&1 == 0 {
				b[j] = 'A'
			} else {
				b[j] = 'C'
			}
		}
		seqs[i] = string(b)
	}
	return seqs
}

func writeSplitFixture(t *testing.T, dir string, gammaFirst bool) (input, taxonkit string) {
	t.Helper()
	gammaSeqs := splitTestGammaSequences()
	var fasta strings.Builder
	if gammaFirst {
		for i, seq := range gammaSeqs {
			fmt.Fprintf(&fasta, ">G%d\n%s\n", i, seq)
		}
		fasta.WriteString(">P1\nACGTACGTAC\n>P2\nACGTACGTAC\n>P3\nTTTTGGCCAA\n>P4\nTTTTGGCCAA\n")
	} else {
		fasta.WriteString(">P2\nACGTACGTAC\n>P1\nACGTACGTAC\n>P3\nTTTTGGCCAA\n>P4\nTTTTGGCCAA\n")
		for i, seq := range gammaSeqs {
			fmt.Fprintf(&fasta, ">G%d\n%s\n", i, seq)
		}
	}
	input = filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(fasta.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	var tsv strings.Builder
	tsv.WriteString("processid\tspecies\n")
	for i := range gammaSeqs {
		fmt.Fprintf(&tsv, "G%d\tGamma\n", i)
	}
	tsv.WriteString("P1\tAlpha\nP2\tBeta\nP3\tAlpha\nP4\tAlpha\n")
	taxonkit = filepath.Join(dir, "taxonkit_input.tsv")
	if err := os.WriteFile(taxonkit, []byte(tsv.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return input, taxonkit
}

func writeSplitTaxdump(t *testing.T, dir string, pids []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ranks := []string{"kingdom", "phylum", "class", "order", "family", "genus", "species"}
	var nodes, names strings.Builder
	for i, rank := range ranks {
		id := i + 1
		parent := id
		if i > 0 {
			parent = i
		}
		fmt.Fprintf(&nodes, "%d\t|\t%d\t|\t%s\t|\n", id, parent, rank)
		fmt.Fprintf(&names, "%d\t|\tRank%d\t|\t\t|\tscientific name\t|\n", id, id)
	}
	if err := os.WriteFile(filepath.Join(dir, "nodes.dmp"), []byte(nodes.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "names.dmp"), []byte(names.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var m strings.Builder
	for _, pid := range pids {
		m.WriteString(pid + "\t7\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "taxid.map"), []byte(m.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runSplitFixture(t *testing.T, dedupe bool, gammaFirst bool) (splitReport, string) {
	t.Helper()
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, gammaFirst)
	pids := []string{"P1", "P2", "P3", "P4"}
	for i := 0; i < 8; i++ {
		pids = append(pids, fmt.Sprintf("G%d", i))
	}
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeSplitTaxdump(t, taxdumpDir, pids)

	outDir := filepath.Join(dir, "libraries")
	if err := SplitOne(Config{
		Input:         input,
		OutDir:        outDir,
		TaxonkitInput: taxonkit,
		Classifiers:   []string{"blast"},
		TaxdumpDir:    taxdumpDir,
		RunQC:         true,
		QC: qc.Config{
			GapPolicy:  qc.GapPolicyDrop,
			DedupeSeqs: dedupe,
			DedupeIDs:  true,
		},
	}); err != nil {
		t.Fatalf("SplitOne: %v", err)
	}

	reportData, err := os.ReadFile(filepath.Join(outDir, "split_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report splitReport
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatal(err)
	}
	return report, outDir
}

func TestSplitRunQCAcceptsDuplicateIDsWithDedupe(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	fasta := ">P1\nACGTACGTAC\n>P1\nACGTACGTAC\n>P3\nTTTTGGCCAA\n>P5\nAAAAACCCCC\n>P6\nGGGGGTTTTT\n"
	if err := os.WriteFile(input, []byte(fasta), 0o644); err != nil {
		t.Fatal(err)
	}
	taxonkit := filepath.Join(dir, "taxonkit_input.tsv")
	if err := os.WriteFile(taxonkit, []byte("processid\tspecies\nP1\tAlpha\nP3\tAlpha\nP5\tAlpha\nP6\tAlpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeSplitTaxdump(t, taxdumpDir, []string{"P1", "P3", "P5", "P6"})

	strategy := DefaultStrategy()
	strategy.MinSeenRecords = 1
	strategy.MinSeenBarcodes = 1
	outDir := filepath.Join(dir, "libraries")
	cfg := Config{
		Input:         input,
		OutDir:        outDir,
		TaxonkitInput: taxonkit,
		Classifiers:   []string{"blast"},
		TaxdumpDir:    taxdumpDir,
		RunQC:         true,
		Strategy:      strategy,
		QC: qc.Config{
			GapPolicy:  qc.GapPolicyDrop,
			DedupeSeqs: true,
			DedupeIDs:  true,
		},
	}
	if err := SplitOne(cfg); err != nil {
		t.Fatalf("SplitOne with duplicate IDs and dedupe: %v", err)
	}

	qcOut, err := os.ReadFile(filepath.Join(outDir, "qc", "input.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(qcOut), ">P1"); got != 1 {
		t.Fatalf("expected duplicate P1 deduplicated exactly once in QC output, found %d:\n%s", got, qcOut)
	}

	cfg.QC.DedupeIDs = false
	if err := SplitOne(cfg); err == nil || !strings.Contains(err.Error(), "duplicate processid") {
		t.Fatalf("expected duplicate-ID rejection without dedupe, got %v", err)
	}
}

func TestSplitConflictExcludedFromTrainingUnderQCDedup(t *testing.T) {
	report, outDir := runSplitFixture(t, true, true)
	stats := report.Stats

	if stats.ConflictGroups != 1 || stats.ConflictRecords != 2 {
		t.Fatalf("unexpected conflict stats: %+v", stats)
	}
	if stats.SeenTrainRecords == 0 {
		t.Fatalf("expected seen species to supply training records: %+v", stats)
	}

	pretrain, err := os.ReadFile(filepath.Join(outDir, "pretrain.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(pretrain), ">") != 1 {
		t.Fatalf("expected exactly one deduped conflicted record in pretrain: %q", pretrain)
	}
	if !strings.Contains(string(pretrain), "ACGTACGTAC") {
		t.Fatalf("conflicted barcode missing from pretrain: %q", pretrain)
	}

	for _, bucket := range []string{"seen_train", "seen_val", "seen_test", "test_unseen", "val_unseen", "keys_unseen", "other_heldout"} {
		data, err := os.ReadFile(filepath.Join(outDir, bucket+".fasta"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "ACGTACGTAC") {
			t.Fatalf("conflicted barcode leaked into %s", bucket)
		}
	}
}

func TestSplitConflictClassificationOrderIndependent(t *testing.T) {
	forward, _ := runSplitFixture(t, true, true)
	reversed, _ := runSplitFixture(t, true, false)
	if forward.Stats != reversed.Stats {
		t.Fatalf("split stats depend on record order:\nforward: %+v\nreversed: %+v", forward.Stats, reversed.Stats)
	}
}

func TestSplitConflictExcludedWithDedupeDisabled(t *testing.T) {
	report, outDir := runSplitFixture(t, false, true)
	stats := report.Stats

	if stats.ConflictGroups != 1 || stats.ConflictRecords != 2 {
		t.Fatalf("unexpected conflict stats: %+v", stats)
	}
	pretrain, err := os.ReadFile(filepath.Join(outDir, "pretrain.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pretrain), ">P1\n") || !strings.Contains(string(pretrain), ">P2\n") {
		t.Fatalf("expected both conflicted records in pretrain: %q", pretrain)
	}
	if strings.Count(string(pretrain), ">") != 2 {
		t.Fatalf("expected exactly two records in pretrain: %q", pretrain)
	}
}

func TestSplitFlushFailurePropagatesAndPreservesOutputs(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGTACGTAC\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "libraries")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(outDir, "seen_train.fasta")
	if err := os.WriteFile(existing, []byte(">OLD\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", filepath.Join(outDir, "seen_val.fasta.tmp")); err != nil {
		t.Fatal(err)
	}

	hash := md5.Sum([]byte("ACGTACGTAC"))
	plan := splitPlan{seqBucket: map[[16]byte]string{hash: bucketSeenVal}}
	_, _, err := writeSplitFastas(input, outDir, plan, map[string]string{"P1": "Alpha"})
	if err == nil || !strings.Contains(err.Error(), "seen_val.fasta") {
		t.Fatalf("expected flush failure for seen_val, got %v", err)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">OLD\nACGT\n" {
		t.Fatalf("existing output not preserved: %q", got)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "seen_train.fasta" {
		t.Fatalf("expected only the preserved output after failure, got %v", names)
	}
}

func TestSplitSetupFailureClosesHandles(t *testing.T) {
	if _, err := os.ReadDir("/proc/self/fd"); err != nil {
		t.Skip("file descriptor inspection unavailable")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGTACGTAC\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "libraries")
	if err := os.MkdirAll(filepath.Join(outDir, "seen_val.fasta.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	hash := md5.Sum([]byte("ACGTACGTAC"))
	plan := splitPlan{seqBucket: map[[16]byte]string{hash: bucketSeenVal}}
	_, _, err := writeSplitFastas(input, outDir, plan, map[string]string{"P1": "Alpha"})
	if err == nil {
		t.Fatal("expected setup error for directory at tmp path")
	}

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && strings.HasPrefix(target, outDir+string(os.PathSeparator)) && strings.HasSuffix(target, ".tmp") {
			t.Fatalf("split writer left open after setup failure: %s", target)
		}
	}
}

func TestSplitPublishesAtomicallyWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGT\n>P2\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "libraries")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(outDir, "pretrain.fasta")
	if err := os.WriteFile(existing, []byte(">OLD\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash := md5.Sum([]byte("ACGT"))
	plan := splitPlan{seqBucket: map[[16]byte]string{hash: bucketPretrain}}
	counts, _, err := writeSplitFastas(input, outDir, plan, map[string]string{"P1": "Alpha", "P2": "Alpha"})
	if err != nil {
		t.Fatalf("writeSplitFastas: %v", err)
	}
	if counts[bucketPretrain] != 2 {
		t.Fatalf("unexpected counts: %+v", counts)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if want := ">P1\nACGT\n>P2\nACGT\n"; string(got) != want {
		t.Fatalf("pretrain not republished: got %q, want %q", got, want)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".bak") {
			t.Fatalf("leftover artifact after successful publish: %s", e.Name())
		}
	}
}

func TestPrunedTaxdumpFlushFailurePropagates(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available")
	}
	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "taxdump")
	writeSplitTaxdump(t, fixtureDir, []string{"P1"})
	dump, err := boldtaxonomy.LoadTaxDump(filepath.Join(fixtureDir, "nodes.dmp"), filepath.Join(fixtureDir, "names.dmp"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		call func(path string) error
	}{
		{"nodes", "nodes.dmp", func(path string) error { return writePrunedNodes(path, dump, map[int]struct{}{7: {}}) }},
		{"names", "names.dmp", func(path string) error { return writePrunedNames(path, dump, map[int]struct{}{7: {}}) }},
		{"taxidmap", "taxid.map", func(path string) error { return writePrunedTaxidMap(path, map[string]int{"P1": 7}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			final := filepath.Join(outDir, tc.path)
			if err := os.Symlink("/dev/full", final+".tmp"); err != nil {
				t.Fatal(err)
			}
			if err := tc.call(final); err == nil {
				t.Fatal("expected flush failure to propagate")
			}
			if _, err := os.Stat(final); !os.IsNotExist(err) {
				t.Fatalf("failed writer published %s", final)
			}
		})
	}
}

func TestBuildSplitPlanDetectsConflictsWithoutQC(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, true)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, DefaultStrategy())
	if err != nil {
		t.Fatalf("buildSplitPlan: %v", err)
	}

	hash := md5.Sum([]byte("ACGTACGTAC"))
	if _, ok := plan.conflicted[hash]; !ok {
		t.Fatalf("conflicted hash missing from plan: %+v", plan.conflicted)
	}
	if stats.ConflictGroups != 1 || stats.ConflictRecords != 2 {
		t.Fatalf("unexpected conflict stats: %+v", stats)
	}
}

func TestSplitReportRecordsEffectiveStrategy(t *testing.T) {
	report, _ := runSplitFixture(t, true, true)
	if want := newStrategyReport(DefaultStrategy()); report.Strategy != want {
		t.Fatalf("report strategy %+v, want %+v", report.Strategy, want)
	}
}

func TestSplitStrategyOverridesBucketPolicies(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, true)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	raiseSeenBar := DefaultStrategy()
	raiseSeenBar.MinSeenRecords = 20
	_, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, raiseSeenBar)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeenClasses != 0 || stats.TotalClasses != 2 {
		t.Fatalf("expected no seen classes with raised bar: %+v", stats)
	}

	noHeldout := DefaultStrategy()
	noHeldout.HeldoutFrac = 0
	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, noHeldout)
	if err != nil {
		t.Fatal(err)
	}
	if stats.UnseenClasses != 1 || stats.HeldoutClasses != 0 {
		t.Fatalf("expected heldout-frac 0 to hold out nothing: %+v", stats)
	}
	for _, bucket := range plan.seqBucket {
		if bucket == bucketHeldout {
			t.Fatalf("expected heldout-frac 0 to leave the %s bucket empty", bucketHeldout)
		}
	}

	allHeldout := DefaultStrategy()
	allHeldout.HeldoutFrac = 1
	plan, stats, err = buildSplitPlan(input, labels, invalidIDs, nil, allHeldout)
	if err != nil {
		t.Fatal(err)
	}
	if stats.UnseenClasses != 0 || stats.HeldoutClasses != 1 {
		t.Fatalf("expected heldout-frac 1 to hold out every non-seen species: %+v", stats)
	}
	for hash, bucket := range plan.seqBucket {
		if bucket == bucketHeldout {
			continue
		}
		if bucket != bucketSeenTrain && bucket != bucketSeenVal && bucket != bucketSeenTest {
			t.Fatalf("expected every non-seen group held out, hash %x in %s", hash, bucket)
		}
	}

	tightTest := DefaultStrategy()
	tightTest.MaxTestTargetPerClass = 1
	plan, stats, err = buildSplitPlan(input, labels, invalidIDs, nil, tightTest)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeenClasses != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	counts := make(map[string]int)
	for _, seq := range splitTestGammaSequences() {
		hash := md5.Sum([]byte(seq))
		counts[plan.seqBucket[hash]]++
	}
	if counts[bucketSeenTest] != 1 || counts[bucketSeenVal] != 1 || counts[bucketSeenTrain] != 6 {
		t.Fatalf("test-cap 1 should leave one test, one val and six train records: %+v", counts)
	}

	outOfRange := DefaultStrategy()
	outOfRange.SeenValTarget = 1.5
	if _, _, err := buildSplitPlan(input, labels, invalidIDs, nil, outOfRange); err == nil {
		t.Fatal("expected out-of-range fraction to fail")
	}
}

func writeTwoGroupFixture(t *testing.T, dir string) (input, taxonkit string) {
	t.Helper()
	var fasta strings.Builder
	for i := 0; i < 7; i++ {
		fmt.Fprintf(&fasta, ">D%d\nACGTACGTAC\n", i)
	}
	fasta.WriteString(">D7\nTTTTGGCCAA\n")
	input = filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(fasta.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	var tsv strings.Builder
	tsv.WriteString("processid\tspecies\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&tsv, "D%d\tDelta\n", i)
	}
	taxonkit = filepath.Join(dir, "taxonkit_input.tsv")
	if err := os.WriteFile(taxonkit, []byte(tsv.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return input, taxonkit
}

func TestSplitIncludeSpeciesForcesSeenWithTrainingRecord(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, true)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	strategy := DefaultStrategy()
	strategy.IncludeSpecies = []string{"Alpha"}
	strategy.HoldoutSpecies = []string{"Alpha"}
	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeenClasses != 2 || stats.ForcedSeenClasses != 1 {
		t.Fatalf("expected Alpha forced seen alongside Gamma: %+v", stats)
	}
	if stats.HeldoutClasses != 0 || stats.ForcedHeldoutClasses != 0 {
		t.Fatalf("include must outrank holdout: %+v", stats)
	}
	hash := md5.Sum([]byte("TTTTGGCCAA"))
	if plan.seqBucket[hash] != bucketSeenTrain {
		t.Fatalf("forced single-barcode species landed in %s, want %s", plan.seqBucket[hash], bucketSeenTrain)
	}
}

func TestSplitIncludeSpeciesGuaranteesTrainingWithTwoGroups(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeTwoGroupFixture(t, dir)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, DefaultStrategy())
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeenClasses != 1 {
		t.Fatalf("expected Delta to qualify as seen: %+v", stats)
	}
	for _, seq := range []string{"ACGTACGTAC", "TTTTGGCCAA"} {
		if plan.seqBucket[md5.Sum([]byte(seq))] == bucketSeenTrain {
			t.Fatal("greedy allocation was expected to leave seen_train empty for a 7+1 species")
		}
	}

	strategy := DefaultStrategy()
	strategy.IncludeSpecies = []string{"Delta"}
	forcedPlan, forcedStats, err := buildSplitPlan(input, labels, invalidIDs, nil, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if forcedStats.SeenClasses != 1 || forcedStats.ForcedSeenClasses != 1 {
		t.Fatalf("unexpected forced stats: %+v", forcedStats)
	}
	trainUnits := 0
	for _, seq := range []string{"ACGTACGTAC", "TTTTGGCCAA"} {
		if forcedPlan.seqBucket[md5.Sum([]byte(seq))] == bucketSeenTrain {
			trainUnits++
		}
	}
	if trainUnits != 1 {
		t.Fatalf("forced species must retain exactly one training unit, got %d", trainUnits)
	}
}

func TestSplitHoldoutSpeciesForcedToHeldout(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, true)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	strategy := DefaultStrategy()
	strategy.HoldoutSpecies = []string{"Gamma", "NoSuchSpecies"}
	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeenClasses != 0 || stats.HeldoutClasses != 1 || stats.ForcedHeldoutClasses != 1 {
		t.Fatalf("expected Gamma forced to heldout: %+v", stats)
	}
	if stats.UnseenClasses != 1 {
		t.Fatalf("expected Alpha to stay unseen under the default policy: %+v", stats)
	}
	if stats.HoldoutUnknown != 1 {
		t.Fatalf("unknown holdout species must be counted: %+v", stats)
	}
	for i, seq := range splitTestGammaSequences() {
		if plan.seqBucket[md5.Sum([]byte(seq))] != bucketHeldout {
			t.Fatalf("G%d not held out", i)
		}
	}
}

func TestBuildSplitPlanDefaultStrategyGoldenBuckets(t *testing.T) {
	dir := t.TempDir()
	input, taxonkit := writeSplitFixture(t, dir, true)

	fastaIDs, err := collectFastaIDs(input, false)
	if err != nil {
		t.Fatal(err)
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkit, fastaIDs)
	if err != nil {
		t.Fatal(err)
	}

	plan, stats, err := buildSplitPlan(input, labels, invalidIDs, nil, DefaultStrategy())
	if err != nil {
		t.Fatalf("buildSplitPlan: %v", err)
	}
	if stats.TotalClasses != 2 || stats.SeenClasses != 1 || stats.UnseenClasses != 1 || stats.HeldoutClasses != 0 {
		t.Fatalf("unexpected class stats: %+v", stats)
	}

	want := map[string]string{
		"G0": bucketSeenTrain,
		"G1": bucketSeenTrain,
		"G2": bucketSeenTest,
		"G3": bucketSeenTrain,
		"G4": bucketSeenTrain,
		"G5": bucketSeenTrain,
		"G6": bucketSeenTest,
		"G7": bucketSeenVal,
		"P3": bucketUnseenTest,
		"P4": bucketUnseenTest,
		"P1": bucketPretrain,
		"P2": bucketPretrain,
	}
	seqs := map[string]string{"P1": "ACGTACGTAC", "P2": "ACGTACGTAC", "P3": "TTTTGGCCAA", "P4": "TTTTGGCCAA"}
	for i, seq := range splitTestGammaSequences() {
		seqs[fmt.Sprintf("G%d", i)] = seq
	}
	for id, wantBucket := range want {
		hash := md5.Sum([]byte(seqs[id]))
		got := bucketPretrain
		if _, conflict := plan.conflicted[hash]; !conflict {
			if mapped, ok := plan.seqBucket[hash]; ok {
				got = mapped
			}
		}
		if got != wantBucket {
			t.Errorf("%s: got bucket %s, want %s", id, got, wantBucket)
		}
	}
}
