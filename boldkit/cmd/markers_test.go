package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
)

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
