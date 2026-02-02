package cmd

import (
	"bufio"
	"crypto/md5"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const unseenClassCutoff = 51

type splitStats struct {
	TotalRecords  int
	TotalClasses  int
	SeenClasses   int
	UnseenClasses int
}

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
	_ = ranks

	labels, err := loadProcessLabelMap(taxonkitIn)
	if err != nil {
		return err
	}
	assignments, stats, err := buildSplitAssignments(input, labels)
	if err != nil {
		return err
	}

	if err := writeSplitFastas(input, outDir, assignments); err != nil {
		return err
	}
	prunedDir, keptTaxids, err := pruneTaxdumpForSeenTrain(assignments, taxdumpDir, taxidMap, outDir)
	if err != nil {
		return err
	}

	logf("split: records=%d classes=%d seen-classes=%d unseen-classes=%d", stats.TotalRecords, stats.TotalClasses, stats.SeenClasses, stats.UnseenClasses)
	logf("split: pruned taxdump -> %s (kept_taxids=%d)", prunedDir, keptTaxids)
	return nil
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

func buildSplitAssignments(input string, labels map[string]string) (map[string]string, splitStats, error) {
	in, err := openInput(input)
	if err != nil {
		return nil, splitStats{}, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	classMembers := make(map[string][]string, 1<<20)
	seenIDs := make(map[string]struct{}, 1<<20)
	stats := splitStats{}

	err = parseFasta(in, func(rec fastaRecord) error {
		if rec.id == "" {
			return fmt.Errorf("found FASTA record with empty ID")
		}
		if _, ok := seenIDs[rec.id]; ok {
			return fmt.Errorf("duplicate processid in input FASTA: %s", rec.id)
		}
		seenIDs[rec.id] = struct{}{}

		label, ok := labels[rec.id]
		if !ok {
			return fmt.Errorf("missing species label for processid %s in taxonkit input", rec.id)
		}
		classMembers[label] = append(classMembers[label], rec.id)
		stats.TotalRecords++
		return nil
	})
	if err != nil {
		return nil, splitStats{}, err
	}
	if stats.TotalRecords == 0 {
		return nil, splitStats{}, fmt.Errorf("input FASTA appears empty: %s", input)
	}

	assignments := make(map[string]string, len(seenIDs))
	stats.TotalClasses = len(classMembers)

	for label, ids := range classMembers {
		if len(ids) == 0 {
			continue
		}

		ordered := stablePIDOrder(ids)
		classUnseen := len(ids) == 1 || classHashByte(label) < unseenClassCutoff
		if classUnseen {
			stats.UnseenClasses++
			testN, valN, keyN := unseenCounts(len(ordered))
			assignRange(assignments, ordered, 0, testN, "unseen_test")
			assignRange(assignments, ordered, testN, testN+valN, "unseen_val")
			assignRange(assignments, ordered, testN+valN, testN+valN+keyN, "unseen_key")
			continue
		}

		stats.SeenClasses++
		trainN, valN, testN := seenCounts(len(ordered))
		assignRange(assignments, ordered, 0, testN, "seen_test")
		assignRange(assignments, ordered, testN, testN+valN, "seen_val")
		assignRange(assignments, ordered, testN+valN, testN+valN+trainN, "seen_train")
	}

	return assignments, stats, nil
}

func writeSplitFastas(input, outDir string, assignments map[string]string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	paths := map[string]string{
		"seen_train":  filepath.Join(outDir, "seen_train.fasta"),
		"seen_val":    filepath.Join(outDir, "seen_val.fasta"),
		"seen_test":   filepath.Join(outDir, "seen_test.fasta"),
		"unseen_test": filepath.Join(outDir, "unseen_test.fasta"),
		"unseen_val":  filepath.Join(outDir, "unseen_val.fasta"),
		"unseen_key":  filepath.Join(outDir, "unseen_key.fasta"),
	}

	type splitWriter struct {
		file *os.File
		buf  *bufio.Writer
	}
	writers := make(map[string]splitWriter, len(paths))
	for key, path := range paths {
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create %s: %w", path, err)
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
		return fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	return parseFasta(in, func(rec fastaRecord) error {
		split, ok := assignments[rec.id]
		if !ok {
			return fmt.Errorf("missing split assignment for processid %s", rec.id)
		}
		w, ok := writers[split]
		if !ok {
			return fmt.Errorf("unknown split bucket %s", split)
		}
		if err := writeFasta(w.buf, rec.id, rec.seq); err != nil {
			return err
		}
		return nil
	})
}

func stablePIDOrder(ids []string) []string {
	type item struct {
		id   string
		hash [16]byte
	}
	items := make([]item, 0, len(ids))
	for _, id := range ids {
		items = append(items, item{
			id:   id,
			hash: md5.Sum([]byte(id)),
		})
	}
	// Order by hash first so splits remain stable as new records are appended.
	sort.Slice(items, func(i, j int) bool {
		if items[i].hash != items[j].hash {
			return lessHash(items[i].hash, items[j].hash)
		}
		return items[i].id < items[j].id
	})

	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].id
	}
	return out
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

func seenCounts(n int) (trainN, valN, testN int) {
	if n < 8 {
		return n, 0, 0
	}
	testN = ceilDiv(2*n, 10)
	if testN > 25 {
		testN = 25
	}
	remaining := n - testN
	valN = ceilDiv(remaining, 20)
	trainN = remaining - valN
	return trainN, valN, testN
}

func unseenCounts(n int) (testN, valN, keyN int) {
	testN = ceilDiv(2*n, 10)
	if testN > 25 {
		testN = 25
	}
	remaining := n - testN
	valN = ceilDiv(remaining, 5)
	keyN = remaining - valN
	return testN, valN, keyN
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func assignRange(assignments map[string]string, ids []string, start, end int, bucket string) {
	if start < 0 {
		start = 0
	}
	if end > len(ids) {
		end = len(ids)
	}
	for i := start; i < end; i++ {
		assignments[ids[i]] = bucket
	}
}

func pruneTaxdumpForSeenTrain(assignments map[string]string, taxdumpDir, taxidMapPath, outDir string) (string, int, error) {
	seenTrainIDs := make(map[string]struct{})
	for pid, bucket := range assignments {
		if bucket == "seen_train" {
			seenTrainIDs[pid] = struct{}{}
		}
	}
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
