package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMarkerInputMatchesSanitizedCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "COI-5P.fasta"), []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MATK.fasta.gz"), []byte("gz"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveMarkerInput(dir, "coi-5p")
	if err != nil {
		t.Fatalf("resolve coi-5p: %v", err)
	}
	if want := filepath.Join(dir, "COI-5P.fasta"); got != want {
		t.Fatalf("resolved %q, want %q", got, want)
	}

	got, err = resolveMarkerInput(dir, "matk")
	if err != nil {
		t.Fatalf("resolve matk: %v", err)
	}
	if want := filepath.Join(dir, "MATK.fasta.gz"); got != want {
		t.Fatalf("resolved %q, want %q", got, want)
	}

	if _, err := resolveMarkerInput(dir, "COI-5P,MATK"); err == nil {
		t.Fatal("expected unsplittable marker name with comma to fail")
	}
	if _, err := resolveMarkerInput(dir, "ITS2"); err == nil {
		t.Fatal("expected missing marker to fail")
	}
}
