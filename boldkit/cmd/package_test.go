package cmd

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageReleaseRejectsExistingOutputsBeforeMovingInputs(t *testing.T) {
	for _, output := range []string{"taxdump", "markers", "taxonomy", "manifest", "checksums"} {
		t.Run(output, func(t *testing.T) {
			cfg := newPackageFixture(t, output == "taxonomy")
			if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{
				"taxdump":   packageTaxdumpArchivePath(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Snapshot),
				"markers":   packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot),
				"taxonomy":  packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot),
				"manifest":  filepath.Join(cfg.ReleaseDir, "manifest.json"),
				"checksums": filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt"),
			}
			path := paths[output]
			previous := "previous release"
			if output == "taxdump" {
				previous = ""
			}
			if err := os.WriteFile(path, []byte(previous), 0o644); err != nil {
				t.Fatal(err)
			}
			before := packageSourceContents(t, cfg)
			err := packageRelease(cfg)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "force") {
				t.Fatalf("expected existing-output error identifying %s, got %v", path, err)
			}
			assertPackageSources(t, before)
			got, err := os.ReadFile(path)
			if err != nil || string(got) != previous {
				t.Fatalf("existing output changed: %q, %v", got, err)
			}
			entries, err := os.ReadDir(cfg.ReleaseDir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("rejection created or moved outputs: %v, %v", entries, err)
			}
		})
	}
}

func TestPackageReleasePublishesCurrentInputs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		compressed bool
		force      bool
	}{
		{name: "plain taxonomy"},
		{name: "gzip taxonomy", compressed: true},
		{name: "replace existing release", force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newPackageFixture(t, tc.compressed)
			cfg.Force = tc.force
			if tc.force {
				if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{
					packageTaxdumpArchivePath(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Snapshot),
					packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot),
					packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot),
					filepath.Join(cfg.ReleaseDir, "manifest.json"),
					filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt"),
				} {
					if err := os.WriteFile(path, []byte("previous release"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := packageRelease(cfg); err != nil {
				t.Fatal(err)
			}
			assertArchiveMember(t, packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot), "markers/COI-5P.fasta", ">P1\nACGT\n")
			assertArchiveMember(t, packageTaxdumpArchivePath(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Snapshot), "taxdump/taxid.map", "P1\t1\n")
			assertPackageGzip(t, packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot), "species\tprocessid\nCanis lupus\tP1\n")
			for _, path := range []string{cfg.TaxdumpDir, cfg.MarkerDir, cfg.TaxonkitOut, filepath.Join(cfg.ReleaseDir, "taxdump"), filepath.Join(cfg.ReleaseDir, "markers")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("expected packaged source to be removed: %s, %v", path, err)
				}
			}
		})
	}
}

func TestPackageReleaseRejectsNonRegularOutputsWithForce(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			cfg := newPackageFixture(t, false)
			cfg.Force = true
			if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot)
			if kind == "directory" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink("missing", path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			before := packageSourceContents(t, cfg)
			if err := packageRelease(cfg); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected output conflict error, got %v", err)
			}
			assertPackageSources(t, before)
		})
	}
}

func TestPackageReleaseKeepsGzipTaxonomyAlreadyAtDestination(t *testing.T) {
	cfg := newPackageFixture(t, true)
	cfg.Snapshot = ""
	if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(cfg.ReleaseDir, filepath.Base(cfg.TaxonkitOut))
	if err := os.Rename(cfg.TaxonkitOut, dest); err != nil {
		t.Fatal(err)
	}
	cfg.TaxonkitOut = dest
	if err := packageRelease(cfg); err != nil {
		t.Fatal(err)
	}
	assertPackageGzip(t, dest, "species\tprocessid\nCanis lupus\tP1\n")
}

func newPackageFixture(t *testing.T, compressed bool) packageConfig {
	t.Helper()
	dir := t.TempDir()
	cfg := packageConfig{
		TaxdumpDir:  filepath.Join(dir, "taxdump"),
		MarkerDir:   filepath.Join(dir, "markers"),
		TaxonkitOut: filepath.Join(dir, "taxonomy.tsv"),
		ReleaseDir:  filepath.Join(dir, "releases"),
		Snapshot:    "fixture",
		MoveInputs:  true,
	}
	for _, path := range []string{cfg.TaxdumpDir, cfg.MarkerDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		filepath.Join(cfg.TaxdumpDir, "nodes.dmp"):   "1\t|\t1\t|\tspecies\t|\n",
		filepath.Join(cfg.TaxdumpDir, "names.dmp"):   "1\t|\tCanis lupus\t|\t\t|\tscientific name\t|\n",
		filepath.Join(cfg.TaxdumpDir, "taxid.map"):   "P1\t1\n",
		filepath.Join(cfg.MarkerDir, "COI-5P.fasta"): ">P1\nACGT\n",
		cfg.TaxonkitOut: "species\tprocessid\nCanis lupus\tP1\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if compressed {
		path := cfg.TaxonkitOut + ".gz"
		if err := packageTaxonkitGzip(cfg.TaxonkitOut, path, false); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(cfg.TaxonkitOut); err != nil {
			t.Fatal(err)
		}
		cfg.TaxonkitOut = path
	}
	return cfg
}

func packageSourceContents(t *testing.T, cfg packageConfig) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	for _, path := range []string{cfg.TaxonkitOut, filepath.Join(cfg.TaxdumpDir, "nodes.dmp"), filepath.Join(cfg.TaxdumpDir, "names.dmp"), filepath.Join(cfg.TaxdumpDir, "taxid.map"), filepath.Join(cfg.MarkerDir, "COI-5P.fasta")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contents[path] = string(data)
	}
	return contents
}

func assertPackageSources(t *testing.T, contents map[string]string) {
	t.Helper()
	for path, want := range contents {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("source changed: %s: %q, %v", path, got, err)
		}
	}
}

func assertPackageGzip(t *testing.T, path, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	got, err := io.ReadAll(gz)
	if err != nil || string(got) != want {
		t.Fatalf("gzip contents: %q, %v", got, err)
	}
}

func assertArchiveMember(t *testing.T, path, member, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == member {
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != want {
				t.Fatalf("archive member %s: %q, %v", member, got, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("missing archive member %s", member)
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		t.Fatal(err)
	}
}
