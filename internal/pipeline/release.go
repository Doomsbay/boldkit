package pipeline

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type ReleaseConfig struct {
	TaxdumpDir    string
	MarkerDir     string
	TaxonkitOut   string
	ReleaseDir    string
	Snapshot      string
	Force         bool
	SkipManifest  bool
	SkipChecksums bool
	MoveInputs    bool
}

func PackageRelease(cfg ReleaseConfig) error {
	util.Logf("Packaging release artifacts -> %s", cfg.ReleaseDir)
	if err := checkPackageOutputs(cfg); err != nil {
		return fmt.Errorf("check release outputs: %w", err)
	}
	if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
		return fmt.Errorf("create releases dir: %w", err)
	}

	taxdumpDir := cfg.TaxdumpDir
	markerDir := cfg.MarkerDir
	taxonkitSource := cfg.TaxonkitOut
	taxonkitRelease := ""
	taxonkitGz := packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot)
	removeTaxonkitPlain := false
	taxonkitIsGz := strings.HasSuffix(cfg.TaxonkitOut, ".gz")

	if cfg.MoveInputs {
		var err error
		taxdumpDir, err = moveDirInto(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Force)
		if err != nil {
			return err
		}
		markerDir, err = moveDirInto(cfg.MarkerDir, cfg.ReleaseDir, cfg.Force)
		if err != nil {
			return err
		}
		taxonkitRelease = packageTaxonkitPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot)
		if err := movePath(cfg.TaxonkitOut, taxonkitRelease, cfg.Force); err != nil {
			return err
		}
		taxonkitSource = taxonkitRelease
		removeTaxonkitPlain = !taxonkitIsGz
	}

	markerZip := packageMarkerPath(markerDir, cfg.ReleaseDir, cfg.Snapshot)
	taxdumpArchive := packageTaxdumpArchivePath(taxdumpDir, cfg.ReleaseDir, cfg.Snapshot)

	util.Logf("Package taxdump archive -> %s", taxdumpArchive)
	if err := boldio.PackageDirGzip(taxdumpDir, taxdumpArchive, cfg.Force); err != nil {
		return err
	}

	util.Logf("Package marker archive -> %s", markerZip)
	if err := boldio.PackageDirGzip(markerDir, markerZip, cfg.Force); err != nil {
		return err
	}

	if !taxonkitIsGz {
		util.Logf("Package taxonkit input gzip -> %s", taxonkitGz)
		if err := packageTaxonkitGzip(taxonkitSource, taxonkitGz, cfg.Force); err != nil {
			return err
		}
	} else if taxonkitSource != taxonkitGz {
		util.Logf("Package taxonkit input gzip -> %s", taxonkitGz)
		if err := boldio.CopyFile(taxonkitSource, taxonkitGz); err != nil {
			return err
		}
	}

	if !cfg.SkipManifest {
		manifestPath := filepath.Join(cfg.ReleaseDir, "manifest.json")
		util.Logf("Write manifest -> %s", manifestPath)
		if err := WriteManifest(manifestPath, taxdumpDir, markerDir, cfg.Snapshot, cfg.Force); err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
	}

	if !cfg.SkipChecksums {
		sumPath := filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt")
		util.Logf("Write checksums -> %s", sumPath)
		if err := WriteChecksums(cfg.ReleaseDir, sumPath, cfg.Force); err != nil {
			return fmt.Errorf("checksums: %w", err)
		}
	}

	if cfg.MoveInputs {
		if removeTaxonkitPlain && taxonkitRelease != "" {
			if err := os.Remove(taxonkitRelease); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove taxonkit input: %w", err)
			}
		}
		if err := os.RemoveAll(taxdumpDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove taxdump dir: %w", err)
		}
		if err := os.RemoveAll(markerDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove marker dir: %w", err)
		}
	}

	return nil
}

func checkPackageOutputs(cfg ReleaseConfig) error {
	paths := []string{
		packageTaxdumpArchivePath(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Snapshot),
		packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot),
	}
	taxonkitGz := packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot)
	if !strings.HasSuffix(cfg.TaxonkitOut, ".gz") || filepath.Clean(cfg.TaxonkitOut) != filepath.Clean(taxonkitGz) {
		paths = append(paths, taxonkitGz)
	}
	if !cfg.SkipManifest {
		paths = append(paths, filepath.Join(cfg.ReleaseDir, "manifest.json"))
	}
	if !cfg.SkipChecksums {
		paths = append(paths, filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt"))
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !cfg.Force {
			return fmt.Errorf("release output exists (use --force to overwrite): %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release output is not a regular file: %s", path)
		}
	}
	return nil
}

func moveDirInto(srcDir, releaseDir string, force bool) (string, error) {
	dest := filepath.Join(releaseDir, filepath.Base(srcDir))
	if err := movePath(srcDir, dest, force); err != nil {
		return "", err
	}
	return dest, nil
}

func movePath(src, dest string, force bool) error {
	if filepath.Clean(src) == filepath.Clean(dest) {
		return nil
	}
	if boldio.PathExists(dest) {
		if !force {
			return fmt.Errorf("destination exists (use --force): %s", dest)
		}
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("remove existing %s: %w", dest, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		if err := boldio.CopyDir(src, dest); err != nil {
			_ = os.RemoveAll(dest)
			return err
		}
	} else {
		if err := boldio.CopyFile(src, dest); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}

func packageMarkerPath(markerDir, releaseDir, snapshot string) string {
	suffix := ""
	if snapshot != "" {
		suffix = "." + util.SafeTag(snapshot)
	}
	markerName := filepath.Base(markerDir) + suffix + ".tar.gz"
	return filepath.Join(releaseDir, markerName)
}

func packageTaxdumpArchivePath(taxdumpDir, releaseDir, snapshot string) string {
	suffix := ""
	if snapshot != "" {
		suffix = "." + util.SafeTag(snapshot)
	}
	base := filepath.Base(taxdumpDir)
	return filepath.Join(releaseDir, base+suffix+".tar.gz")
}

func packageTaxonkitPath(taxonkitOut, releaseDir, snapshot string) string {
	base := filepath.Base(taxonkitOut)
	if snapshot != "" {
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		base = name + "." + util.SafeTag(snapshot) + ext
	}
	return filepath.Join(releaseDir, base)
}

func packageTaxonkitGzipPath(taxonkitOut, releaseDir, snapshot string) string {
	base := filepath.Base(taxonkitOut)
	if snapshot != "" {
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		base = name + "." + util.SafeTag(snapshot) + ext
	}
	if !strings.HasSuffix(base, ".gz") {
		base += ".gz"
	}
	return filepath.Join(releaseDir, base)
}

func packageTaxonkitGzip(src, dest string, force bool) error {
	if filepath.Clean(src) == filepath.Clean(dest) {
		util.Logf("taxonkit gzip already in release dir: %s", dest)
		return nil
	}
	if boldio.FileExists(dest) && !force {
		util.Logf("taxonkit gzip exists, skipping (use --force to overwrite): %s", dest)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create release dir: %w", err)
	}
	if strings.HasSuffix(src, ".gz") {
		return boldio.CopyFile(src, dest)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open taxonkit input: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create taxonkit gzip: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	gzw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(gzw, in); err != nil {
		_ = gzw.Close()
		return fmt.Errorf("gzip taxonkit input: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("finalize gzip: %w", err)
	}
	return nil
}

func WriteChecksums(releaseDir, outputFile string, force bool) error {
	if boldio.FileExists(outputFile) && !force {
		util.Logf("checksums exist, skipping (use --force to overwrite): %s", outputFile)
		return nil
	}

	patterns := []string{"*.zip", "*.tar.gz", "*.tsv.gz"}
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(releaseDir, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			seen[match] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("no packaged files found in %s", releaseDir)
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)

	out, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	for _, f := range files {
		sum, err := boldio.SHA256File(f)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s  %s\n", sum, filepath.Base(f)); err != nil {
			return err
		}
	}
	return nil
}

func WriteManifest(path, taxdumpDir, markerDir, snapshot string, force bool) error {
	if boldio.FileExists(path) && !force {
		util.Logf("manifest exists, skipping (use --force to overwrite): %s", path)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	commit := "unknown"
	if c, err := gitCommitHash(); err == nil && c != "" {
		commit = c
	}

	nodes, err := boldio.CountLines(filepath.Join(taxdumpDir, "nodes.dmp"))
	if err != nil {
		return err
	}
	names, err := boldio.CountLines(filepath.Join(taxdumpDir, "names.dmp"))
	if err != nil {
		return err
	}
	taxid, err := boldio.CountLines(filepath.Join(taxdumpDir, "taxid.map"))
	if err != nil {
		return err
	}

	markerFiles, err := listMarkerFiles(markerDir)
	if err != nil {
		return err
	}
	markerSeqs, err := countMarkerSeqs(markerFiles)
	if err != nil {
		return err
	}

	manifest := struct {
		SnapshotID string `json:"snapshot_id"`
		CommitHash string `json:"commit_hash"`
		Counts     struct {
			Nodes                int `json:"nodes"`
			Names                int `json:"names"`
			TaxidMap             int `json:"taxid_map"`
			MarkerFastaFiles     int `json:"marker_fasta_files"`
			MarkerFastaSequences int `json:"marker_fasta_sequences"`
		} `json:"counts"`
	}{
		SnapshotID: snapshot,
		CommitHash: commit,
	}
	manifest.Counts.Nodes = nodes
	manifest.Counts.Names = names
	manifest.Counts.TaxidMap = taxid
	manifest.Counts.MarkerFastaFiles = len(markerFiles)
	manifest.Counts.MarkerFastaSequences = markerSeqs

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func listMarkerFiles(markerDir string) ([]string, error) {
	var files []string
	err := filepath.Walk(markerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".fasta") || strings.HasSuffix(info.Name(), ".fasta.gz") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func countMarkerSeqs(paths []string) (int, error) {
	total := 0
	for _, p := range paths {
		rc, err := boldio.OpenInput(p)
		if err != nil {
			return 0, fmt.Errorf("open %s: %w", p, err)
		}
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) > 0 && line[0] == '>' {
				total++
			}
		}
		_ = rc.Close()
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("scan %s: %w", p, err)
		}
	}
	return total, nil
}
