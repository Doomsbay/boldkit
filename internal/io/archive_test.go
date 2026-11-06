package io

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackageDirGzipPublishesValidArchive(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "library")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "blast.fasta"), []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "library.tar.gz")

	if err := PackageDirGzip(srcDir, dest, false); err != nil {
		t.Fatalf("PackageDirGzip: %v", err)
	}

	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(gzipB(t, f))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("walk archive: %v", err)
		}
		names = append(names, hdr.Name)
	}
	if len(names) != 1 || names[0] != "library/blast.fasta" {
		t.Fatalf("archive entries = %v, want [library/blast.fasta]", names)
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after publish")
	}
}

func TestPackageDirGzipFailureKeepsPreviousArchive(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "library")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "blast.fasta"), []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(srcDir, "dangling")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "library.tar.gz")
	previous := []byte("previous archive")
	if err := os.WriteFile(dest, previous, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PackageDirGzip(srcDir, dest, true); err == nil {
		t.Fatal("expected packaging failure on unreadable source entry")
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(previous) {
		t.Errorf("previous archive replaced by partial output: %q", got)
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after failed packaging")
	}
}

func TestPackageDirGzipSkipsExistingArchive(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "library.tar.gz")
	if err := os.WriteFile(dest, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PackageDirGzip(filepath.Join(dir, "missing-src"), dest, false); err != nil {
		t.Fatalf("skip-if-exists must not touch the source: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep me" {
		t.Errorf("existing archive modified on skip: %q", got)
	}
}

func gzipB(t *testing.T, r io.Reader) io.Reader {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	return gz
}
