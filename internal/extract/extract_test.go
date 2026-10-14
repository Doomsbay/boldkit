package extract

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseAuditClosesFileAfterFlushError(t *testing.T) {
	f, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("/dev/full unavailable: %v", err)
	}
	defer func() { _ = f.Close() }()
	writer := bufio.NewWriterSize(f, 64)
	if _, err := writer.WriteString("audit row\n"); err != nil {
		t.Fatal(err)
	}
	curator := &Bioscan5MCurator{auditFile: f, auditWriter: writer}
	if err := curator.closeAudit(); err == nil || !strings.Contains(err.Error(), "flush audit") {
		t.Fatalf("expected flush error, got %v", err)
	}
	if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("audit file not closed: %v", err)
	}
	if err := curator.closeAudit(); err != nil {
		t.Fatalf("repeated cleanup: %v", err)
	}
}

func TestBuildTaxonkitErrorLeavesNoOutputs(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "out.tsv")
	content := "processid\tbin_uri\tkingdom\nP1\t\tAnimalia\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTaxonkit(input, output, 0, -1, CurationConfig{}.Normalized()); err == nil {
		t.Fatal("expected error for missing headers")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "input.tsv" {
		t.Fatalf("expected only the input file, got %v", entryNames(entries))
	}
}

func TestBuildTaxonkitPublishFailurePreservesTarget(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "out.tsv")
	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\t\tAnimalia\tChordata\tMammalia\tCarnivora\tCanidae\t\t\tCanis\t",
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := BuildTaxonkit(input, output, 0, -1, CurationConfig{}.Normalized())
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !strings.Contains(err.Error(), "publish output") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected no temp leftovers, got %v", entryNames(entries))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestBuildTaxonkitProcessIDFallbackByProtocol(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	outputNone := filepath.Join(tmp, "out_none.tsv")
	outputBioscan := filepath.Join(tmp, "out_bioscan.tsv")

	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\t\tAnimalia\tChordata\tMammalia\tCarnivora\tCanidae\t\t\tCanis\t",
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if _, err := BuildTaxonkit(input, outputNone, 0, -1, CurationConfig{Protocol: ProtocolNone}.Normalized()); err != nil {
		t.Fatalf("BuildTaxonkit none failed: %v", err)
	}
	dataNone, err := os.ReadFile(outputNone)
	if err != nil {
		t.Fatalf("read none output: %v", err)
	}
	if !strings.Contains(string(dataNone), "Canis sp. P1\tP1\n") {
		t.Fatalf("expected PROCESSID fallback in none mode, got:\n%s", string(dataNone))
	}

	if _, err := BuildTaxonkit(input, outputBioscan, 0, -1, CurationConfig{Protocol: ProtocolBioscan5M}.Normalized()); err != nil {
		t.Fatalf("BuildTaxonkit bioscan failed: %v", err)
	}
	dataBioscan, err := os.ReadFile(outputBioscan)
	if err != nil {
		t.Fatalf("read bioscan output: %v", err)
	}
	if strings.Contains(string(dataBioscan), "Canis sp. P1\tP1\n") {
		t.Fatalf("did not expect PROCESSID fallback in bioscan mode, got:\n%s", string(dataBioscan))
	}
	if !strings.Contains(string(dataBioscan), "\tCanis\t\tP1\n") {
		t.Fatalf("expected empty species in bioscan mode when BIN is missing, got:\n%s", string(dataBioscan))
	}
}
