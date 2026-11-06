package cmd

import (
	"os"
	"path/filepath"
	"strings"
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

func TestResolveMarkerInputRejectsAmbiguousSpellings(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"COI-5P.fasta.gz", "coi-5p.fasta.gz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := resolveMarkerInput(dir, "COI-5P")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "COI-5P.fasta.gz") || !strings.Contains(err.Error(), "coi-5p.fasta.gz") {
		t.Fatalf("expected ambiguous-marker error naming both files, got %v", err)
	}

	mixed := filepath.Join(dir, "mixed")
	if err := os.MkdirAll(mixed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixed, "MATK.fasta"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixed, "matk.fasta"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveMarkerInput(mixed, "matk"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous-marker error across plain and gz spellings, got %v", err)
	}
}
