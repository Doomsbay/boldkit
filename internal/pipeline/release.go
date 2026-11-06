package pipeline

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
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
	Version       string
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
	if err := checkPackageInputs(cfg); err != nil {
		return fmt.Errorf("check release inputs: %w", err)
	}
	if err := os.MkdirAll(cfg.ReleaseDir, 0o755); err != nil {
		return fmt.Errorf("create releases dir: %w", err)
	}

	taxonkitGz := packageTaxonkitGzipPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot)
	taxonkitIsGz := strings.HasSuffix(cfg.TaxonkitOut, ".gz")

	taxdumpArchive := packageTaxdumpArchivePath(cfg.TaxdumpDir, cfg.ReleaseDir, cfg.Snapshot)
	util.Logf("Package taxdump archive -> %s", taxdumpArchive)
	if err := boldio.PackageDirGzip(cfg.TaxdumpDir, taxdumpArchive, cfg.Force); err != nil {
		return err
	}

	markerZip := packageMarkerPath(cfg.MarkerDir, cfg.ReleaseDir, cfg.Snapshot)
	util.Logf("Package marker archive -> %s", markerZip)
	if err := boldio.PackageDirGzip(cfg.MarkerDir, markerZip, cfg.Force); err != nil {
		return err
	}

	if !taxonkitIsGz {
		util.Logf("Package taxonkit input gzip -> %s", taxonkitGz)
		if err := packageTaxonkitGzip(cfg.TaxonkitOut, taxonkitGz, cfg.Force); err != nil {
			return err
		}
	} else if !cfg.MoveInputs && filepath.Clean(cfg.TaxonkitOut) != filepath.Clean(taxonkitGz) {
		util.Logf("Copy taxonkit input gzip -> %s", taxonkitGz)
		if err := boldio.CopyFile(cfg.TaxonkitOut, taxonkitGz); err != nil {
			return err
		}
	}

	taxonkitArtifact := ReleaseFile{Path: taxonkitGz, Name: filepath.Base(taxonkitGz)}
	if taxonkitIsGz && cfg.MoveInputs {
		taxonkitArtifact.Path = cfg.TaxonkitOut
	}
	releaseFiles := []ReleaseFile{
		{Path: taxdumpArchive, Name: filepath.Base(taxdumpArchive)},
		{Path: markerZip, Name: filepath.Base(markerZip)},
		taxonkitArtifact,
	}
	if !cfg.SkipManifest {
		manifestPath := filepath.Join(cfg.ReleaseDir, "manifest.json")
		util.Logf("Write manifest -> %s", manifestPath)
		if err := WriteManifest(manifestPath, cfg.TaxdumpDir, cfg.MarkerDir, cfg.Snapshot, cfg.Version, cfg.Force); err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
		releaseFiles = append(releaseFiles, ReleaseFile{Path: manifestPath, Name: "manifest.json"})
	}

	if !cfg.SkipChecksums {
		sumPath := filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt")
		util.Logf("Write checksums -> %s", sumPath)
		if err := WriteChecksums(sumPath, releaseFiles, cfg.Force); err != nil {
			return fmt.Errorf("checksums: %w", err)
		}
	}

	if cfg.MoveInputs {
		if taxonkitIsGz {
			taxonkitRelease := packageTaxonkitPath(cfg.TaxonkitOut, cfg.ReleaseDir, cfg.Snapshot)
			if err := movePath(cfg.TaxonkitOut, taxonkitRelease, cfg.Force); err != nil {
				return fmt.Errorf("move taxonkit input: %w", err)
			}
		} else if err := os.Remove(cfg.TaxonkitOut); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove taxonkit input: %w", err)
		}
		if err := os.RemoveAll(cfg.TaxdumpDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove taxdump dir: %w", err)
		}
		if err := os.RemoveAll(cfg.MarkerDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove marker dir: %w", err)
		}
	}

	return nil
}

func checkPackageInputs(cfg ReleaseConfig) error {
	for _, name := range []string{"nodes.dmp", "names.dmp", "taxid.map"} {
		path := filepath.Join(cfg.TaxdumpDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("missing taxdump input: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("taxdump input is not a regular file: %s", path)
		}
	}
	markerFiles, err := listMarkerFiles(cfg.MarkerDir)
	if err != nil {
		return fmt.Errorf("list marker inputs: %w", err)
	}
	if len(markerFiles) == 0 {
		return fmt.Errorf("no marker FASTA files found in %s", cfg.MarkerDir)
	}
	info, err := os.Stat(cfg.TaxonkitOut)
	if err != nil {
		return fmt.Errorf("missing taxonomy input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("taxonomy input is not a regular file: %s", cfg.TaxonkitOut)
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
	base := snapshotTaggedName(filepath.Base(taxonkitOut), snapshot)
	return filepath.Join(releaseDir, base)
}

func packageTaxonkitGzipPath(taxonkitOut, releaseDir, snapshot string) string {
	base := snapshotTaggedName(filepath.Base(taxonkitOut), snapshot)
	if !strings.HasSuffix(base, ".gz") {
		base += ".gz"
	}
	return filepath.Join(releaseDir, base)
}

func snapshotTaggedName(base, snapshot string) string {
	if snapshot == "" {
		return base
	}
	ext := filepath.Ext(base)
	if strings.HasSuffix(base, ".tsv.gz") {
		ext = ".tsv.gz"
	}
	return strings.TrimSuffix(base, ext) + "." + util.SafeTag(snapshot) + ext
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

	out, err := boldio.CreateAtomic(dest)
	if err != nil {
		return fmt.Errorf("create taxonkit gzip: %w", err)
	}

	gzw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		out.Abort()
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(gzw, in); err != nil {
		_ = gzw.Close()
		out.Abort()
		return fmt.Errorf("gzip taxonkit input: %w", err)
	}
	if err := gzw.Close(); err != nil {
		out.Abort()
		return fmt.Errorf("finalize gzip: %w", err)
	}
	if err := out.Commit(); err != nil {
		return fmt.Errorf("publish taxonkit gzip: %w", err)
	}
	return nil
}

type ReleaseFile struct {
	Path string
	Name string
}

func WriteChecksums(outputFile string, files []ReleaseFile, force bool) error {
	if boldio.FileExists(outputFile) && !force {
		util.Logf("checksums exist, skipping (use --force to overwrite): %s", outputFile)
		return nil
	}

	out, err := boldio.CreateAtomic(outputFile)
	if err != nil {
		return fmt.Errorf("create checksums: %w", err)
	}
	for _, f := range files {
		sum, err := boldio.SHA256File(f.Path)
		if err != nil {
			out.Abort()
			return err
		}
		if _, err := fmt.Fprintf(out, "%s  %s\n", sum, f.Name); err != nil {
			out.Abort()
			return fmt.Errorf("write checksums: %w", err)
		}
	}
	if err := out.Commit(); err != nil {
		return fmt.Errorf("publish checksums: %w", err)
	}
	return nil
}

func WriteManifest(path, taxdumpDir, markerDir, snapshot, version string, force bool) error {
	if boldio.FileExists(path) && !force {
		util.Logf("manifest exists, skipping (use --force to overwrite): %s", path)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
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
		CommitHash: releaseCommitHash(version),
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
	out, err := boldio.CreateAtomic(path)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		out.Abort()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := out.Commit(); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}
	return nil
}

func releaseCommitHash(version string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		revision := ""
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
		if revision != "" {
			if modified {
				revision += "-dirty"
			}
			return revision
		}
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	if version != "" && version != "dev" {
		return version
	}
	return "unknown"
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
