package io

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFileCopiesContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fasta")
	dest := filepath.Join(dir, "dest.fasta")
	if err := os.WriteFile(src, []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n" {
		t.Errorf("copied content = %q", got)
	}
}

func TestCopyFileFailureKeepsSourceAndPreviousDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fasta")
	if err := os.WriteFile(src, []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest.fasta")
	previous := []byte("previous copy")
	if err := os.WriteFile(dest, previous, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", dest+".tmp"); err != nil {
		t.Skipf("/dev/full unavailable: %v", err)
	}

	if err := CopyFile(src, dest); err == nil {
		t.Fatal("expected copy failure on /dev/full")
	}

	got, err := os.ReadFile(src)
	if err != nil || string(got) != ">P1\nACGT\n" {
		t.Fatalf("source damaged by failed copy: %q, %v", got, err)
	}
	gotDest, err := os.ReadFile(dest)
	if err != nil || string(gotDest) != string(previous) {
		t.Fatalf("previous destination lost by failed copy: %q, %v", gotDest, err)
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
}
