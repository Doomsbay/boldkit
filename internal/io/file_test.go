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

func TestCopyFileFailureKeepsSourceAndDestClean(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fasta")
	if err := os.WriteFile(src, []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest.fasta")
	if err := os.Symlink("/dev/full", dest); err != nil {
		t.Skipf("/dev/full unavailable: %v", err)
	}

	if err := CopyFile(src, dest); err == nil {
		t.Fatal("expected copy failure on /dev/full")
	}

	got, err := os.ReadFile(src)
	if err != nil || string(got) != ">P1\nACGT\n" {
		t.Fatalf("source damaged by failed copy: %q, %v", got, err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Errorf("partial destination left behind: %v", err)
	}
}
