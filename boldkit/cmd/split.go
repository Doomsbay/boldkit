package cmd

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	bucketSeenTrain  = "seen_train"
	bucketSeenVal    = "seen_val"
	bucketSeenTest   = "seen_test"
	bucketUnseenTest = "test_unseen"
	bucketUnseenVal  = "val_unseen"
	bucketUnseenKeys = "keys_unseen"
	bucketHeldout    = "other_heldout"
	bucketPretrain   = "pretrain"
)

type splitStats struct {
	TotalRecords     int `json:"total_records"`
	TotalClasses     int `json:"total_classes"`
	SeenClasses      int `json:"seen_classes"`
	UnseenClasses    int `json:"unseen_classes"`
	HeldoutClasses   int `json:"heldout_classes"`
	SeenTrainRecords int `json:"seen_train_records"`
	SeenValRecords   int `json:"seen_val_records"`
	SeenTestRecords  int `json:"seen_test_records"`
	UnseenTest       int `json:"test_unseen_records"`
	UnseenVal        int `json:"val_unseen_records"`
	UnseenKey        int `json:"keys_unseen_records"`
	HeldoutRecords   int `json:"other_heldout_records"`
	PretrainRecords  int `json:"pretrain_records"`
}

type splitReport struct {
	Input       string    `json:"input"`
	OutDir      string    `json:"out_dir"`
	Classifiers []string  `json:"classifiers"`
	PrunedTaxa  int       `json:"pruned_taxids"`
	Stats       splitStats `json:"stats"`
}

type splitQCConfig struct {
	Enabled    bool
	MinLen     int
	MaxLen     int
	MaxN       int
	MaxAmbig   int
	MaxInvalid int
	DedupeSeqs bool
	DedupeIDs  bool
	Progress   bool
}

type barcodeUnit struct {
	hash  [16]byte
	count int
}

type barcodeGroup struct {
	label    string
	count    int
	conflict bool
}

type splitPlan struct {
	seqBucket  map[[16]byte]string
	conflicted map[[16]byte]struct{}
	invalidIDs map[string]struct{}
}

type splitTarget struct {
	bucket string
	target int
}

func runSplit(args []string) {
	fs := flag.NewFlagSet("split", flag.ExitOnError)
	input := fs.String("input", "", "Input FASTA/FASTA.gz")
	outDir := fs.String("outdir", "libraries", "Output directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Marker FASTA directory (used when -input is empty)")
	markers := fs.String("markers", "COI-5P", "Comma-separated markers to process (used when -input is empty)")
	classifiers := fs.String("classifier", "blast,kraken2,sintax", "Comma-separated classifiers for final reference formatting")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Taxdump directory with nodes.dmp/names.dmp/taxid.map")
	taxidMap := fs.String("taxid-map", "", "Optional taxid.map override")
	taxonkitIn := fs.String("taxonkit-input", "taxonkit_input.tsv", "Taxonkit TSV with processid/species labels")
	requireRanks := fs.String("require-ranks", "kingdom,phylum,class,order,family,genus,species", "Comma-separated ranks required to keep a sequence (empty disables)")
	runQC := fs.Bool("run-qc", true, "Run QC before splitting")
	qcMin := fs.Int("qc-min-length", 200, "QC minimum cleaned length")
	qcMax := fs.Int("qc-max-length", 700, "QC maximum cleaned length")
	qcMaxN := fs.Int("qc-max-n", 0, "QC maximum N count")
	qcMaxAmbig := fs.Int("qc-max-ambig", 0, "QC maximum IUPAC ambiguous count")
	qcMaxInvalid := fs.Int("qc-max-invalid", 0, "QC maximum invalid character count")
	qcDedupe := fs.Bool("qc-dedupe", true, "QC drop duplicate sequences")
	qcDedupeIDs := fs.Bool("qc-dedupe-ids", true, "QC drop duplicate IDs")
	qcProgress := fs.Bool("qc-progress", true, "Show QC progress bar (approximate)")
	formatProgress := fs.Bool("format-progress", true, "Show format progress bar (approximate)")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	ranks := splitList(*requireRanks)
	classifierList := splitList(*classifiers)
	if len(classifierList) == 0 {
		fatalf("classifier must not be empty")
	}
	qcCfg := splitQCConfig{
		Enabled:    *runQC,
		MinLen:     *qcMin,
		MaxLen:     *qcMax,
		MaxN:       *qcMaxN,
		MaxAmbig:   *qcMaxAmbig,
		MaxInvalid: *qcMaxInvalid,
		DedupeSeqs: *qcDedupe,
		DedupeIDs:  *qcDedupeIDs,
		Progress:   *qcProgress,
	}

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
			if err := splitOne(markerInput, baseOut, *taxonkitIn, ranks, classifierList, *taxdumpDir, *taxidMap, qcCfg, *formatProgress); err != nil {
				fatalf("split %s failed: %v", marker, err)
			}
		}
		return
	}

	if err := splitOne(*input, *outDir, *taxonkitIn, ranks, classifierList, *taxdumpDir, *taxidMap, qcCfg, *formatProgress); err != nil {
		fatalf("split failed: %v", err)
	}
}

func splitOne(input, outDir, taxonkitIn string, ranks, classifiers []string, taxdumpDir, taxidMap string, qcCfg splitQCConfig, formatProgress bool) error {
	splitInput := input
	if qcCfg.Enabled {
		qcOut := filepath.Join(outDir, "qc", qcBaseName(input)+".fasta")
		logf("split: QC -> %s", qcOut)
		if err := qcFasta(input, qcConfig{
			MinLen:       qcCfg.MinLen,
			MaxLen:       qcCfg.MaxLen,
			MaxN:         qcCfg.MaxN,
			MaxAmbig:     qcCfg.MaxAmbig,
			MaxInvalid:   qcCfg.MaxInvalid,
			DedupeSeqs:   qcCfg.DedupeSeqs,
			DedupeIDs:    qcCfg.DedupeIDs,
			RequireRanks: ranks,
			TaxdumpDir:   taxdumpDir,
			TaxidMapPath: taxidMap,
			OutputPath:   qcOut,
			Progress:     qcCfg.Progress,
		}); err != nil {
			return fmt.Errorf("qc failed: %w", err)
		}
		splitInput = qcOut
	}

	fastaIDs, err := collectFastaIDs(splitInput)
	if err != nil {
		return err
	}
	labels, invalidIDs, err := loadProcessLabelMap(taxonkitIn, fastaIDs)
	if err != nil {
		return err
	}

	plan, stats, err := buildSplitPlan(splitInput, labels, invalidIDs)
	if err != nil {
		return err
	}

	writeStats, seenTrainIDs, err := writeSplitFastas(splitInput, outDir, plan, labels)
	if err != nil {
		return err
	}
	stats.SeenTrainRecords = writeStats[bucketSeenTrain]
	stats.SeenValRecords = writeStats[bucketSeenVal]
	stats.SeenTestRecords = writeStats[bucketSeenTest]
	stats.UnseenTest = writeStats[bucketUnseenTest]
	stats.UnseenVal = writeStats[bucketUnseenVal]
	stats.UnseenKey = writeStats[bucketUnseenKeys]
	stats.HeldoutRecords = writeStats[bucketHeldout]
	stats.PretrainRecords = writeStats[bucketPretrain]

	prunedDir, keptTaxids, err := pruneTaxdumpForSeenTrain(seenTrainIDs, taxdumpDir, taxidMap, outDir)
	if err != nil {
		return err
	}

	seenTrain := filepath.Join(outDir, "seen_train.fasta")
	formatOut := filepath.Join(outDir, "formatted")
	logf("split: format references from %s -> %s", seenTrain, formatOut)
	if err := formatFasta(formatConfig{
		Classifiers:  classifiers,
		RequireRanks: ranks,
		Input:        seenTrain,
		OutDir:       formatOut,
		TaxdumpDir:   prunedDir,
		TaxidMapPath: filepath.Join(prunedDir, "taxid.map"),
		Progress:     formatProgress,
	}); err != nil {
		return fmt.Errorf("format references: %w", err)
	}

	logf("split: records=%d classes=%d seen-classes=%d unseen-classes=%d heldout-classes=%d", stats.TotalRecords, stats.TotalClasses, stats.SeenClasses, stats.UnseenClasses, stats.HeldoutClasses)
	logf("split: pruned taxdump -> %s (kept_taxids=%d)", prunedDir, keptTaxids)
	reportPath := filepath.Join(outDir, "split_report.json")
	if err := writeSplitReport(reportPath, splitReport{
		Input:       splitInput,
		OutDir:      outDir,
		Classifiers: classifiers,
		PrunedTaxa:  keptTaxids,
		Stats:       stats,
	}); err != nil {
		return err
	}
	logf("split: report -> %s", reportPath)
	return nil
}

func collectFastaIDs(input string) (map[string]struct{}, error) {
	in, err := openInput(input)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	ids := make(map[string]struct{}, 1<<20)
	err = parseFasta(in, func(rec fastaRecord) error {
		if rec.id == "" {
			return fmt.Errorf("found FASTA record with empty ID")
		}
		if _, dup := ids[rec.id]; dup {
			return fmt.Errorf("duplicate processid in input FASTA: %s", rec.id)
		}
		ids[rec.id] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("input FASTA appears empty: %s", input)
	}
	return ids, nil
}

func loadProcessLabelMap(path string, wantedIDs map[string]struct{}) (map[string]string, map[string]struct{}, error) {
	in, err := openInput(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open taxonkit input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	opts := DefaultOptions()
	headerSeen := false
	idxProcess := -1
	idxSpecies := -1
	labels := make(map[string]string, len(wantedIDs))
	invalid := make(map[string]struct{})
	found := 0

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
		if _, need := wantedIDs[pid]; !need {
			return nil
		}

		if isNone(row.Fields[idxSpecies]) || len(row.Fields[idxSpecies]) == 0 {
			invalid[pid] = struct{}{}
			return nil
		}
		label := string(row.Fields[idxSpecies])
		if prev, ok := labels[pid]; ok && prev != label {
			return fmt.Errorf("line %d: processid %s maps to multiple labels (%s, %s)", row.Line, pid, prev, label)
		}
		labels[pid] = label
		found++
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	for pid := range wantedIDs {
		if _, ok := labels[pid]; ok {
			continue
		}
		if _, bad := invalid[pid]; bad {
			continue
		}
		invalid[pid] = struct{}{}
	}
	if found == 0 {
		return nil, nil, fmt.Errorf("taxonkit input has no matching process IDs for input FASTA: %s", path)
	}
	if len(invalid) > 0 {
		logf("split: %d records missing species label (moved to %s)", len(invalid), bucketPretrain)
	}
	return labels, invalid, nil
}

func buildSplitPlan(input string, labels map[string]string, invalidIDs map[string]struct{}) (splitPlan, splitStats, error) {
	in, err := openInput(input)
	if err != nil {
		return splitPlan{}, splitStats{}, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	barcodeGroups := make(map[[16]byte]barcodeGroup, 1<<20)
	stats := splitStats{}

	err = parseFasta(in, func(rec fastaRecord) error {
		stats.TotalRecords++
		if _, bad := invalidIDs[rec.id]; bad {
			return nil
		}
		label, ok := labels[rec.id]
		if !ok {
			invalidIDs[rec.id] = struct{}{}
			return nil
		}

		hash := md5.Sum(rec.seq)
		group := barcodeGroups[hash]
		if group.count == 0 {
			group.label = label
		} else if group.label != label {
			group.conflict = true
		}
		group.count++
		barcodeGroups[hash] = group
		return nil
	})
	if err != nil {
		return splitPlan{}, splitStats{}, err
	}

	seqBucket := make(map[[16]byte]string, len(barcodeGroups))
	conflicted := make(map[[16]byte]struct{})
	speciesUnits := make(map[string][]barcodeUnit)
	speciesCounts := make(map[string]int)

	for hash, group := range barcodeGroups {
		if group.conflict {
			conflicted[hash] = struct{}{}
			continue
		}
		speciesUnits[group.label] = append(speciesUnits[group.label], barcodeUnit{hash: hash, count: group.count})
		speciesCounts[group.label] += group.count
	}

	stats.TotalClasses = len(speciesUnits)
	for label, units := range speciesUnits {
		total := speciesCounts[label]
		uniqueBarcodes := len(units)
		sort.Slice(units, func(i, j int) bool {
			return lessHash(units[i].hash, units[j].hash)
		})

		if total >= 8 && uniqueBarcodes >= 2 {
			stats.SeenClasses++
			testTarget := minInt(25, ceilDiv(2*total, 10))
			valTarget := ceilDiv(total-testTarget, 20)
			assignUnits(seqBucket, units, []splitTarget{
				{bucket: bucketSeenTest, target: testTarget},
				{bucket: bucketSeenVal, target: valTarget},
				{bucket: bucketSeenTrain, target: -1},
			})
			continue
		}

		if classHashByte(label) < 128 {
			stats.UnseenClasses++
			testTarget := minInt(25, ceilDiv(2*total, 10))
			valTarget := ceilDiv(total-testTarget, 5)
			assignUnits(seqBucket, units, []splitTarget{
				{bucket: bucketUnseenTest, target: testTarget},
				{bucket: bucketUnseenVal, target: valTarget},
				{bucket: bucketUnseenKeys, target: -1},
			})
			continue
		}

		stats.HeldoutClasses++
		for _, unit := range units {
			seqBucket[unit.hash] = bucketHeldout
		}
	}

	if len(conflicted) > 0 {
		logf("split: %d barcode groups span multiple species labels (moved to %s)", len(conflicted), bucketPretrain)
	}

	return splitPlan{
		seqBucket:  seqBucket,
		conflicted: conflicted,
		invalidIDs: invalidIDs,
	}, stats, nil
}

func assignUnits(seqBucket map[[16]byte]string, units []barcodeUnit, targets []splitTarget) {
	idx := 0
	for _, t := range targets {
		if idx >= len(units) {
			return
		}
		if t.target < 0 {
			for idx < len(units) {
				seqBucket[units[idx].hash] = t.bucket
				idx++
			}
			return
		}
		acc := 0
		for idx < len(units) && acc < t.target {
			seqBucket[units[idx].hash] = t.bucket
			acc += units[idx].count
			idx++
		}
	}
}

func writeSplitFastas(input, outDir string, plan splitPlan, labels map[string]string) (map[string]int, map[string]struct{}, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create output dir: %w", err)
	}

	paths := map[string]string{
		bucketSeenTrain:  filepath.Join(outDir, "seen_train.fasta"),
		bucketSeenVal:    filepath.Join(outDir, "seen_val.fasta"),
		bucketSeenTest:   filepath.Join(outDir, "seen_test.fasta"),
		bucketUnseenTest: filepath.Join(outDir, "test_unseen.fasta"),
		bucketUnseenVal:  filepath.Join(outDir, "val_unseen.fasta"),
		bucketUnseenKeys: filepath.Join(outDir, "keys_unseen.fasta"),
		bucketHeldout:    filepath.Join(outDir, "other_heldout.fasta"),
		bucketPretrain:   filepath.Join(outDir, "pretrain.fasta"),
	}

	type splitWriter struct {
		file *os.File
		buf  *bufio.Writer
	}
	writers := make(map[string]splitWriter, len(paths))
	for key, path := range paths {
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, fmt.Errorf("create %s: %w", path, err)
		}
		writers[key] = splitWriter{
			file: f,
			buf:  bufio.NewWriterSize(f, writerBufferSize),
		}
	}
	defer func() {
		for _, w := range writers {
			_ = w.buf.Flush()
			_ = w.file.Close()
		}
	}()

	in, err := openInput(input)
	if err != nil {
		return nil, nil, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	counts := make(map[string]int)
	seenTrainIDs := make(map[string]struct{})
	err = parseFasta(in, func(rec fastaRecord) error {
		bucket := bucketPretrain
		if _, bad := plan.invalidIDs[rec.id]; !bad {
			if _, ok := labels[rec.id]; ok {
				hash := md5.Sum(rec.seq)
				if _, conflict := plan.conflicted[hash]; !conflict {
					if mapped, ok := plan.seqBucket[hash]; ok {
						bucket = mapped
					}
				}
			}
		}

		w, ok := writers[bucket]
		if !ok {
			return fmt.Errorf("unknown split bucket %s", bucket)
		}
		if err := writeFasta(w.buf, rec.id, rec.seq); err != nil {
			return err
		}
		counts[bucket]++
		if bucket == bucketSeenTrain {
			seenTrainIDs[rec.id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return counts, seenTrainIDs, nil
}

func lessHash(a, b [16]byte) bool {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func classHashByte(label string) byte {
	sum := md5.Sum([]byte(label))
	return sum[0]
}

func ceilDiv(a, b int) int {
	if b <= 0 || a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeSplitReport(path string, report splitReport) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create split report: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("write split report: %w", err)
	}
	return nil
}

func pruneTaxdumpForSeenTrain(seenTrainIDs map[string]struct{}, taxdumpDir, taxidMapPath, outDir string) (string, int, error) {
	if len(seenTrainIDs) == 0 {
		return "", 0, fmt.Errorf("no seen_train sequences found; cannot prune taxdump")
	}

	if taxidMapPath == "" {
		taxidMapPath = filepath.Join(taxdumpDir, "taxid.map")
	}
	pidToTaxid, err := loadTaxidMap(taxidMapPath)
	if err != nil {
		return "", 0, err
	}

	nodesPath := filepath.Join(taxdumpDir, "nodes.dmp")
	namesPath := filepath.Join(taxdumpDir, "names.dmp")
	dump, err := loadTaxDump(nodesPath, namesPath)
	if err != nil {
		return "", 0, err
	}

	keep := make(map[int]struct{}, len(seenTrainIDs)*2)
	seenTrainTaxids := make(map[string]int, len(seenTrainIDs))
	for pid := range seenTrainIDs {
		taxid, ok := pidToTaxid[pid]
		if !ok {
			return "", 0, fmt.Errorf("taxid not found for seen_train processid %s", pid)
		}
		seenTrainTaxids[pid] = taxid

		cur := taxid
		for depth := 0; depth < 128 && cur > 0; depth++ {
			if _, done := keep[cur]; done {
				break
			}
			keep[cur] = struct{}{}
			node, ok := dump.nodes[cur]
			if !ok {
				break
			}
			if node.parent == cur || node.parent <= 0 {
				break
			}
			cur = node.parent
		}
	}

	prunedDir := filepath.Join(outDir, "taxdump_pruned")
	if err := os.MkdirAll(prunedDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create pruned taxdump dir: %w", err)
	}

	if err := writePrunedNodes(filepath.Join(prunedDir, "nodes.dmp"), dump.nodes, keep); err != nil {
		return "", 0, err
	}
	if err := writePrunedNames(filepath.Join(prunedDir, "names.dmp"), dump.nodes, keep); err != nil {
		return "", 0, err
	}
	if err := writePrunedTaxidMap(filepath.Join(prunedDir, "taxid.map"), seenTrainTaxids); err != nil {
		return "", 0, err
	}

	return prunedDir, len(keep), nil
}

func writePrunedNodes(path string, nodes map[int]taxNode, keep map[int]struct{}) error {
	ids := sortedIntSet(keep)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create nodes.dmp: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	w := bufio.NewWriterSize(f, writerBufferSize)
	defer func() {
		_ = w.Flush()
	}()

	for _, id := range ids {
		node, ok := nodes[id]
		if !ok {
			continue
		}
		if _, err := w.WriteString(strconv.Itoa(id) + "\t|\t" + strconv.Itoa(node.parent) + "\t|\t" + node.rank + "\t|\n"); err != nil {
			return fmt.Errorf("write nodes.dmp: %w", err)
		}
	}
	return nil
}

func writePrunedNames(path string, nodes map[int]taxNode, keep map[int]struct{}) error {
	ids := sortedIntSet(keep)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create names.dmp: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	w := bufio.NewWriterSize(f, writerBufferSize)
	defer func() {
		_ = w.Flush()
	}()

	for _, id := range ids {
		node, ok := nodes[id]
		if !ok || node.name == "" {
			continue
		}
		if _, err := w.WriteString(strconv.Itoa(id) + "\t|\t" + node.name + "\t|\t\t|\tscientific name\t|\n"); err != nil {
			return fmt.Errorf("write names.dmp: %w", err)
		}
	}
	return nil
}

func writePrunedTaxidMap(path string, pidTaxid map[string]int) error {
	pids := make([]string, 0, len(pidTaxid))
	for pid := range pidTaxid {
		pids = append(pids, pid)
	}
	sort.Strings(pids)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create taxid.map: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	w := bufio.NewWriterSize(f, writerBufferSize)
	defer func() {
		_ = w.Flush()
	}()

	for _, pid := range pids {
		if _, err := w.WriteString(pid + "\t" + strconv.Itoa(pidTaxid[pid]) + "\n"); err != nil {
			return fmt.Errorf("write taxid.map: %w", err)
		}
	}
	return nil
}

func sortedIntSet(values map[int]struct{}) []int {
	out := make([]int, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
