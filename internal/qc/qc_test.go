package qc

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestQCDuplicateIDNotReservedByRejectedRecord(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nA\n>P1\nACGT\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "clean.fasta")
	report := filepath.Join(dir, "report.json")
	cfg := Config{
		MinLen:     4,
		DedupeSeqs: true,
		DedupeIDs:  true,
		OutputPath: output,
		ReportPath: report,
	}
	if err := RunQC(input, cfg); err != nil {
		t.Fatalf("RunQC: %v", err)
	}
	stats := readQCStats(t, report)
	if stats.TooShort != 1 || stats.Written != 1 || stats.DupeID != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n" {
		t.Fatalf("got %q, want %q", got, ">P1\nACGT\n")
	}
}

func TestQCDuplicateIDCountedAmongAccepted(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n>P1\nTGCA\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "report.json")
	cfg := Config{
		DedupeSeqs: true,
		DedupeIDs:  true,
		OutputPath: filepath.Join(dir, "clean.fasta"),
		ReportPath: report,
	}
	if err := RunQC(input, cfg); err != nil {
		t.Fatalf("RunQC: %v", err)
	}
	stats := readQCStats(t, report)
	if stats.Written != 1 || stats.DupeID != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestQCFlushErrorPropagates(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		DedupeSeqs: true,
		DedupeIDs:  true,
		OutputPath: "/dev/full",
	}
	if err := RunQC(input, cfg); err == nil {
		t.Fatal("expected flush error for /dev/full output")
	}
}

func TestQCDuplicateIDDoesNotReserveSequence(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n>P1\nTGCA\n>P2\nTGCA\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "clean.fasta")
	report := filepath.Join(dir, "report.json")
	cfg := Config{
		DedupeSeqs: true,
		DedupeIDs:  true,
		OutputPath: output,
		ReportPath: report,
	}
	if err := RunQC(input, cfg); err != nil {
		t.Fatalf("RunQC: %v", err)
	}
	stats := readQCStats(t, report)
	if stats.Written != 2 || stats.DupeID != 1 || stats.DupeSeq != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n>P2\nTGCA\n" {
		t.Fatalf("got %q, want %q", got, ">P1\nACGT\n>P2\nTGCA\n")
	}
}

func TestQCDuplicateSeqDoesNotReserveID(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n>P2\nACGT\n>P2\nTGCA\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "clean.fasta")
	report := filepath.Join(dir, "report.json")
	cfg := Config{
		DedupeSeqs: true,
		DedupeIDs:  true,
		OutputPath: output,
		ReportPath: report,
	}
	if err := RunQC(input, cfg); err != nil {
		t.Fatalf("RunQC: %v", err)
	}
	stats := readQCStats(t, report)
	if stats.Written != 2 || stats.DupeSeq != 1 || stats.DupeID != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n>P2\nTGCA\n" {
		t.Fatalf("got %q, want %q", got, ">P1\nACGT\n>P2\nTGCA\n")
	}
}

func TestQCRejectsOutputAliasingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{OutputPath: input}
	if err := RunQC(input, cfg); err == nil {
		t.Fatal("expected error for output path aliasing the input")
	}
	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("input was modified: got %q, want %q", got, content)
	}
}

func TestQCRejectsSymlinkAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.fasta")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	cfg := Config{OutputPath: link}
	if err := RunQC(input, cfg); err == nil {
		t.Fatal("expected error for symlink output aliasing the input")
	}
	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("input was modified: got %q, want %q", got, content)
	}
}

func TestQCRejectsHardlinkAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.fasta")
	if err := os.Link(input, link); err != nil {
		t.Fatal(err)
	}
	cfg := Config{OutputPath: link}
	if err := RunQC(input, cfg); err == nil {
		t.Fatal("expected error for hardlink output aliasing the input")
	}
	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("input was modified: got %q, want %q", got, content)
	}
}

func TestQCRejectsAbsoluteSpellingOfRelativeInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	content := ">P1\nACGT\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	cfg := Config{OutputPath: input}
	if err := RunQC("input.fasta", cfg); err == nil {
		t.Fatal("expected error for absolute output spelling of relative input")
	}
	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("input was modified: got %q, want %q", got, content)
	}
}

func TestQCPreservesOutputWhenTaxonomyInvalid(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "clean.fasta")
	existing := ">P0\nAACC\n"
	if err := os.WriteFile(output, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		OutputPath:   output,
		TaxidMapPath: filepath.Join(dir, "missing", "taxid.map"),
	}
	if err := RunQC(input, cfg); err == nil {
		t.Fatal("expected error for missing taxid map")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("existing output was erased: got %q, want %q", got, existing)
	}
}

func TestQCAcceptsDistinctOutputWithGzipInput(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(">P1\nACGT\n")); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.fasta.gz")
	if err := os.WriteFile(input, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "clean.fasta")
	report := filepath.Join(dir, "report.json")
	cfg := Config{
		OutputPath: output,
		ReportPath: report,
	}
	if err := RunQC(input, cfg); err != nil {
		t.Fatalf("RunQC: %v", err)
	}
	stats := readQCStats(t, report)
	if stats.Written != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func readQCStats(t *testing.T, report string) Stats {
	t.Helper()
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var stats Stats
	if err := json.Unmarshal(data, &stats); err != nil {
		t.Fatal(err)
	}
	return stats
}
