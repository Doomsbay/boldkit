package classify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyCompressStopsBeforeWorkWhenArchiveExists(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "blast.tar.gz")
	if err := os.WriteFile(archive, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Input:       filepath.Join(dir, "missing-input.fasta"),
		OutDir:      dir,
		Classifiers: []string{"blast"},
		Compress:    true,
	}
	err := Classify(cfg)
	if err == nil || !strings.Contains(err.Error(), "blast.tar.gz") {
		t.Fatalf("expected archive-exists error, got %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "blast.tar.gz" {
			continue
		}
		t.Fatalf("work performed before archive check: %s created", entry.Name())
	}
}

func TestClassifyCompressForceProceedsToQC(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "blast.tar.gz")
	if err := os.WriteFile(archive, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Input:       filepath.Join(dir, "missing-input.fasta"),
		OutDir:      dir,
		Classifiers: []string{"blast"},
		Compress:    true,
		Force:       true,
	}
	err := Classify(cfg)
	if err == nil {
		t.Fatal("expected QC failure on missing input")
	}
	if strings.Contains(err.Error(), "archive exists") {
		t.Fatalf("force must not trip the archive check: %v", err)
	}
}
