package split

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
)

func pruneTaxdumpForSeenTrain(seenTrainIDs map[string]struct{}, taxdumpDir, taxidMapPath, outDir string) (string, int, error) {
	if len(seenTrainIDs) == 0 {
		return "", 0, fmt.Errorf("no seen_train sequences found; cannot prune taxdump")
	}

	if taxidMapPath == "" {
		taxidMapPath = filepath.Join(taxdumpDir, "taxid.map")
	}
	pidToTaxid, err := boldtaxonomy.LoadTaxidMap(taxidMapPath)
	if err != nil {
		return "", 0, err
	}

	nodesPath := filepath.Join(taxdumpDir, "nodes.dmp")
	namesPath := filepath.Join(taxdumpDir, "names.dmp")
	dump, err := boldtaxonomy.LoadTaxDump(nodesPath, namesPath)
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
			node, ok := dump.Node(cur)
			if !ok {
				break
			}
			if node.Parent == cur || node.Parent <= 0 {
				break
			}
			cur = node.Parent
		}
	}

	prunedDir := filepath.Join(outDir, "taxdump_pruned")
	if err := os.MkdirAll(prunedDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create pruned taxdump dir: %w", err)
	}

	if err := writePrunedNodes(filepath.Join(prunedDir, "nodes.dmp"), dump, keep); err != nil {
		return "", 0, err
	}
	if err := writePrunedNames(filepath.Join(prunedDir, "names.dmp"), dump, keep); err != nil {
		return "", 0, err
	}
	if err := writePrunedTaxidMap(filepath.Join(prunedDir, "taxid.map"), seenTrainTaxids); err != nil {
		return "", 0, err
	}

	return prunedDir, len(keep), nil
}

func writePrunedFile(path string, write func(*bufio.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	w := bufio.NewWriterSize(f, writeBufferSize)
	if err := write(w); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("flush %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish %s: %w", path, err)
	}
	return nil
}

func writePrunedNodes(path string, dump *boldtaxonomy.TaxDump, keep map[int]struct{}) error {
	ids := sortedIntSet(keep)
	return writePrunedFile(path, func(w *bufio.Writer) error {
		for _, id := range ids {
			node, ok := dump.Node(id)
			if !ok {
				continue
			}
			if _, err := w.WriteString(strconv.Itoa(id) + "\t|\t" + strconv.Itoa(node.Parent) + "\t|\t" + node.Rank + "\t|\n"); err != nil {
				return fmt.Errorf("write nodes.dmp: %w", err)
			}
		}
		return nil
	})
}

func writePrunedNames(path string, dump *boldtaxonomy.TaxDump, keep map[int]struct{}) error {
	ids := sortedIntSet(keep)
	return writePrunedFile(path, func(w *bufio.Writer) error {
		for _, id := range ids {
			node, ok := dump.Node(id)
			if !ok || node.Name == "" {
				continue
			}
			if _, err := w.WriteString(strconv.Itoa(id) + "\t|\t" + node.Name + "\t|\t\t|\tscientific name\t|\n"); err != nil {
				return fmt.Errorf("write names.dmp: %w", err)
			}
		}
		return nil
	})
}

func writePrunedTaxidMap(path string, pidTaxid map[string]int) error {
	pids := make([]string, 0, len(pidTaxid))
	for pid := range pidTaxid {
		pids = append(pids, pid)
	}
	sort.Strings(pids)
	return writePrunedFile(path, func(w *bufio.Writer) error {
		for _, pid := range pids {
			if _, err := w.WriteString(pid + "\t" + strconv.Itoa(pidTaxid[pid]) + "\n"); err != nil {
				return fmt.Errorf("write taxid.map: %w", err)
			}
		}
		return nil
	})
}

func sortedIntSet(values map[int]struct{}) []int {
	out := make([]int, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
