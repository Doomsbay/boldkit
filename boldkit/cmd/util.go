package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const writerBufferSize = 1 << 20

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
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
