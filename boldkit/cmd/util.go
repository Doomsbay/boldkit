package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func outputsExist(outDir string) bool {
	if _, err := os.Stat(outDir); err != nil {
		return false
	}
	if files, _ := filepath.Glob(filepath.Join(outDir, "*.fasta")); len(files) > 0 {
		return true
	}
	if files, _ := filepath.Glob(filepath.Join(outDir, "*.fasta.gz")); len(files) > 0 {
		return true
	}
	return false
}

func snapshotID(inputPath string) string {
	base := filepath.Base(inputPath)
	if strings.HasSuffix(base, ".parquet") {
		return strings.TrimSuffix(base, ".parquet")
	}
	if strings.HasSuffix(base, ".parq") {
		return strings.TrimSuffix(base, ".parq")
	}
	if strings.HasSuffix(base, ".tsv.gz") {
		return strings.TrimSuffix(base, ".tsv.gz")
	}
	if strings.HasSuffix(base, ".tsv") {
		return strings.TrimSuffix(base, ".tsv")
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func updateByteProgress(bar *boldprogress.ByteProgress, counter *boldio.CountReader, last *int64) {
	if bar == nil || counter == nil || last == nil {
		return
	}
	cur := counter.Count()
	delta := cur - *last
	if delta <= 0 {
		return
	}
	bar.Add(delta)
	*last = cur
}
