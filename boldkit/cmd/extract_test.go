package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	if _, err := buildTaxonkit(input, outputNone, 0, -1, extractCurationConfig{Protocol: extractCurationProtocolNone}.normalized()); err != nil {
		t.Fatalf("buildTaxonkit none failed: %v", err)
	}
	dataNone, err := os.ReadFile(outputNone)
	if err != nil {
		t.Fatalf("read none output: %v", err)
	}
	if !strings.Contains(string(dataNone), "Canis sp. P1\tP1\n") {
		t.Fatalf("expected PROCESSID fallback in none mode, got:\n%s", string(dataNone))
	}

	if _, err := buildTaxonkit(input, outputBioscan, 0, -1, extractCurationConfig{Protocol: extractCurationProtocolBioscan5M}.normalized()); err != nil {
		t.Fatalf("buildTaxonkit bioscan failed: %v", err)
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
