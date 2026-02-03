package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBioscanCurateSubfamilyHoleAndEpithetOnlySpecies(t *testing.T) {
	curatorRaw, err := newExtractBioscan5MCurator(extractCurationConfig{Protocol: extractCurationProtocolBioscan5M}, "")
	if err != nil {
		t.Fatalf("newExtractBioscan5MCurator failed: %v", err)
	}
	curator := curatorRaw.(*bioscan5MCurator)

	rec := &extractTaxonRecord{
		Family:    "Crambidae",
		Subfamily: "None",
		Tribe:     "Haimbachiini",
		Genus:     "Homo",
		Species:   "sapiens",
		BinURI:    "BOLD:AAA0001",
	}

	if err := curator.Curate(rec); err != nil {
		t.Fatalf("Curate failed: %v", err)
	}
	if rec.Subfamily != "Crambidae subfam. incertae sedis" {
		t.Fatalf("subfamily=%q want %q", rec.Subfamily, "Crambidae subfam. incertae sedis")
	}
	if rec.Species != "Homo sapiens" {
		t.Fatalf("species=%q want %q", rec.Species, "Homo sapiens")
	}
}

func TestBioscanCurateBinCanonicalAdoption(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "output.tsv")
	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\tBOLD:BIN1\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sapiens",
		"P2\tBOLD:BIN1\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sp. BOLD:BIN1",
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if _, err := buildTaxonkit(input, output, 0, -1, extractCurationConfig{Protocol: extractCurationProtocolBioscan5M}.normalized()); err != nil {
		t.Fatalf("buildTaxonkit failed: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "Homo\tHomo sapiens\tP2\n") {
		t.Fatalf("expected P2 species to adopt BIN canonical species, got:\n%s", got)
	}
}

func TestBioscanCurateGenusMismatchFallsBackToBinProvisional(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "output.tsv")
	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\tBOLD:BIN2\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sapiens",
		"P2\tBOLD:BIN2\tAnimalia\tChordata\tMammalia\tCarnivora\tCanidae\t\t\tCanis\tHomo sapiens",
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if _, err := buildTaxonkit(input, output, 0, -1, extractCurationConfig{Protocol: extractCurationProtocolBioscan5M}.normalized()); err != nil {
		t.Fatalf("buildTaxonkit failed: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "Canis\tCanis sp. BOLD:BIN2\tP2\n") {
		t.Fatalf("expected P2 species to demote to genus+BIN provisional label, got:\n%s", got)
	}
}
