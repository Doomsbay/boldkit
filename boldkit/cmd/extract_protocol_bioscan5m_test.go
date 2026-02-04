package cmd

import (
	"encoding/json"
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

func TestBioscanCurateDoesNotAdoptConflictedBinSpecies(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "output.tsv")
	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\tBOLD:BIN3\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sapiens",
		"P2\tBOLD:BIN3\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo erectus",
		"P3\tBOLD:BIN3\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sp. BOLD:BIN3",
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
	if strings.Contains(got, "Homo\tHomo sapiens\tP3\n") || strings.Contains(got, "Homo\tHomo erectus\tP3\n") {
		t.Fatalf("did not expect conflicted BIN canonical adoption for P3, got:\n%s", got)
	}
	if !strings.Contains(got, "Homo\tHomo sp. BOLD:BIN3\tP3\n") {
		t.Fatalf("expected P3 to remain BIN-provisional under conflict, got:\n%s", got)
	}
}

func TestBioscanReportAndAuditOutputs(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input.tsv")
	output := filepath.Join(tmp, "output.tsv")
	report := filepath.Join(tmp, "curation_report.json")
	audit := filepath.Join(tmp, "curation_audit.tsv")

	content := strings.Join([]string{
		"processid\tbin_uri\tkingdom\tphylum\tclass\torder\tfamily\tsubfamily\ttribe\tgenus\tspecies",
		"P1\tBOLD:BIN4\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\t\t\tHomo\tHomo sapiens",
		"P2\tBOLD:BIN4\tAnimalia\tChordata\tMammalia\tPrimates\tHominidae\tNone\t\tHomo\tsp.",
	}, "\n") + "\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	cfg := extractCurationConfig{
		Protocol:   extractCurationProtocolBioscan5M,
		ReportPath: report,
		AuditPath:  audit,
	}.normalized()
	if _, err := buildTaxonkit(input, output, 0, -1, cfg); err != nil {
		t.Fatalf("buildTaxonkit failed: %v", err)
	}

	reportBytes, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var parsed bioscanCurationReport
	if err := json.Unmarshal(reportBytes, &parsed); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if parsed.Protocol != extractCurationProtocolBioscan5M {
		t.Fatalf("report protocol=%q want %q", parsed.Protocol, extractCurationProtocolBioscan5M)
	}
	if parsed.Stats.RowsTotal != 2 {
		t.Fatalf("report rows_total=%d want 2", parsed.Stats.RowsTotal)
	}
	if parsed.BinSummary.Observed != 1 {
		t.Fatalf("report bin observed=%d want 1", parsed.BinSummary.Observed)
	}

	auditBytes, err := os.ReadFile(audit)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected audit with header and at least one changed row, got:\n%s", string(auditBytes))
	}
	if !strings.Contains(string(auditBytes), "P2\tBOLD:BIN4") {
		t.Fatalf("expected P2 change in audit, got:\n%s", string(auditBytes))
	}
}
