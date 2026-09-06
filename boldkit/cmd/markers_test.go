package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
)

func TestMarkersWarningsUseOriginalMarkerCodes(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	content := "processid\tmarker_code\tnuc\nP1\tCOI/5P\tACGT\nP2\tCOI:5P\tTGCA\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "stderr.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = logFile
	t.Cleanup(func() {
		os.Stderr = stderr
		_ = logFile.Close()
	})
	runMarkers([]string{"-input", input, "-outdir", outDir, "-markers", "coi/5p,COI:5P,COI_5P,MATK", "-progress=false", "-workers=2", "-gzip=false"})
	assertMarkerFasta(t, outDir, "COI_5P", false, ">P1\nACGT\n>P2\nTGCA\n")
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "[boldkit] markers: warning: selected marker \"COI_5P\" matched 0 records\n" +
		"[boldkit] markers: warning: selected marker \"MATK\" matched 0 records\n" +
		"[boldkit] markers: wrote 2 records across 1 marker(s)\n"
	if string(logs) != want {
		t.Fatalf("unexpected marker diagnostics:\ngot: %s\nwant: %s", logs, want)
	}
}

func TestMarkersBuildMissingSelection(t *testing.T) {
	for _, gzipOut := range []bool{false, true} {
		for _, selection := range []string{"MATK", "coi-5p,matk"} {
			t.Run(selection+"/gzip="+strconv.FormatBool(gzipOut), func(t *testing.T) {
				dir := t.TempDir()
				input := filepath.Join(dir, "input.tsv")
				outDir := filepath.Join(dir, "markers")
				writeMarkerFixture(t, input, "ACGT", "TGCA")
				runMarkers([]string{"-input", input, "-outdir", outDir, "-markers", "COI-5P", "-progress=false", "-workers=2", "-gzip=" + strconv.FormatBool(gzipOut)})
				writeMarkerFixture(t, input, "AAAA", "CCCC")
				runMarkers([]string{"-input", input, "-outdir", outDir, "-markers", selection, "-progress=false", "-workers=2", "-gzip=" + strconv.FormatBool(!gzipOut)})
				assertMarkerFasta(t, outDir, "COI-5P", gzipOut, ">P1\nACGT\n")
				assertMarkerFasta(t, outDir, "MATK", !gzipOut, ">P2\nCCCC\n")
				entries, err := os.ReadDir(outDir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 2 {
					t.Fatalf("expected two marker files, got %d", len(entries))
				}
				runMarkers([]string{"-input", filepath.Join(dir, "missing.tsv"), "-outdir", outDir, "-markers", "coi-5p,matk", "-progress=false"})
				runMarkers([]string{"-input", input, "-outdir", outDir, "-markers", "coi-5p", "-progress=false", "-workers=2", "-gzip=" + strconv.FormatBool(gzipOut), "-force"})
				assertMarkerFasta(t, outDir, "COI-5P", gzipOut, ">P1\nAAAA\n")
				assertMarkerFasta(t, outDir, "MATK", !gzipOut, ">P2\nCCCC\n")
			})
		}
	}
}

func TestPipelineBuildMissingMarkerSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	taxonkitOut := filepath.Join(dir, "taxonkit_input.tsv")
	taxdumpDir := filepath.Join(dir, "taxdump")
	if err := os.MkdirAll(taxdumpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{taxonkitOut, filepath.Join(taxdumpDir, "nodes.dmp"), filepath.Join(taxdumpDir, "names.dmp"), filepath.Join(taxdumpDir, "taxid.map")} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeMarkerFixture(t, input, "ACGT", "TGCA")
	runMarkers([]string{"-input", input, "-outdir", outDir, "-markers", "COI-5P", "-progress=false", "-workers=2", "-gzip=false"})
	writeMarkerFixture(t, input, "AAAA", "CCCC")
	runPipeline([]string{"-input", input, "-taxonkit-output", taxonkitOut, "-taxdump-dir", taxdumpDir, "-taxonkit-bin", filepath.Join(dir, "unused-taxonkit"), "-marker-dir", outDir, "-markers", "coi-5p,matk", "-progress=false", "-workers=2", "-no-gzip"})
	assertMarkerFasta(t, outDir, "COI-5P", false, ">P1\nACGT\n")
	assertMarkerFasta(t, outDir, "MATK", false, ">P2\nCCCC\n")
}

func writeMarkerFixture(t *testing.T, path, coi, matk string) {
	t.Helper()
	content := "processid\tmarker_code\tnuc\nP1\tCOI-5P\t" + coi + "\nP2\tMATK\t" + matk + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertMarkerFasta(t *testing.T, dir, marker string, compressed bool, want string) {
	t.Helper()
	path := filepath.Join(dir, marker+".fasta")
	if compressed {
		path += ".gz"
	}
	in, err := boldio.OpenInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	got, err := io.ReadAll(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s: got %q, want %q", path, got, want)
	}
}
