package io

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicFileCommitPublishesContent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")

	af, err := CreateAtomic(dest)
	if err != nil {
		t.Fatalf("create atomic: %v", err)
	}
	if _, err := af.Write([]byte("new content")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := af.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read published file: %v", err)
	}
	if string(got) != "new content" {
		t.Errorf("published content = %q, want %q", got, "new content")
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after commit")
	}
}

func TestAtomicFileFailedWriteKeepsPreviousFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(dest, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", dest+".tmp"); err != nil {
		t.Fatal(err)
	}

	af, err := CreateAtomic(dest)
	if err != nil {
		t.Fatalf("create atomic: %v", err)
	}
	if _, err := af.Write([]byte("partial")); err == nil {
		t.Fatal("expected write to /dev/full to fail")
	}
	af.Abort()

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read previous file: %v", err)
	}
	if string(got) != "previous" {
		t.Errorf("previous file content = %q, want %q", got, "previous")
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after failed write")
	}
}

func TestAtomicFileCommitRenameFailureCleansTemp(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.d")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	af, err := CreateAtomic(dest)
	if err != nil {
		t.Fatalf("create atomic: %v", err)
	}
	if _, err := af.Write([]byte("content")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := af.Commit(); err == nil {
		t.Fatal("expected rename onto a directory to fail")
	}

	if info, err := os.Lstat(dest); err != nil || !info.IsDir() {
		t.Errorf("destination directory was disturbed: %v", err)
	}
	if _, err := os.Lstat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after failed commit")
	}
}
