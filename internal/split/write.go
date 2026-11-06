package split

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
)

const writeBufferSize = 1 << 20

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
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil, nil, fmt.Errorf("output %s is a directory", path)
		}
	}

	type splitWriter struct {
		file  *os.File
		buf   *bufio.Writer
		tmp   string
		final string
	}
	writers := make(map[string]*splitWriter, len(paths))
	cleanup := func() {
		for _, w := range writers {
			if w.file != nil {
				_ = w.file.Close()
			}
			if w.tmp != "" {
				_ = os.Remove(w.tmp)
			}
		}
	}
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()
	for key, path := range paths {
		tmp := path + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return nil, nil, fmt.Errorf("create %s: %w", tmp, err)
		}
		writers[key] = &splitWriter{file: f, buf: bufio.NewWriterSize(f, writeBufferSize), tmp: tmp, final: path}
	}

	in, err := boldio.OpenInput(input)
	if err != nil {
		return nil, nil, fmt.Errorf("open input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	counts := make(map[string]int)
	seenTrainIDs := make(map[string]struct{})
	hasher := barcodeHasher{canonical: plan.canonical}
	err = boldio.ParseFasta(in, func(rec boldio.FASTARecord) error {
		bucket := bucketPretrain
		if _, bad := plan.invalidIDs[rec.ID]; !bad {
			if _, ok := labels[rec.ID]; ok {
				hash := hasher.hash(rec.Seq)
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
		if err := boldio.WriteFasta(w.buf, rec.ID, rec.Seq); err != nil {
			return err
		}
		counts[bucket]++
		if bucket == bucketSeenTrain {
			seenTrainIDs[rec.ID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	for _, w := range writers {
		if err := w.buf.Flush(); err != nil {
			return nil, nil, fmt.Errorf("flush %s: %w", w.final, err)
		}
		if err := w.file.Close(); err != nil {
			return nil, nil, fmt.Errorf("close %s: %w", w.final, err)
		}
		w.file = nil
	}

	backups := make(map[string]string, len(writers))
	for _, w := range writers {
		if _, err := os.Stat(w.final); err == nil {
			bak := w.final + ".bak"
			if err := os.Rename(w.final, bak); err != nil {
				return nil, nil, fmt.Errorf("backup %s: %w", w.final, err)
			}
			backups[w.final] = bak
		}
	}
	published := make([]string, 0, len(writers))
	for _, w := range writers {
		if err := os.Rename(w.tmp, w.final); err != nil {
			for _, path := range published {
				_ = os.Remove(path)
			}
			for final, bak := range backups {
				_ = os.Rename(bak, final)
			}
			return nil, nil, fmt.Errorf("publish %s: %w", w.final, err)
		}
		published = append(published, w.final)
		w.tmp = ""
	}
	for _, bak := range backups {
		_ = os.Remove(bak)
	}
	committed = true

	return counts, seenTrainIDs, nil
}
