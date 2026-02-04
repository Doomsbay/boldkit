package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func runPipeline(args []string) {
	fs := flag.NewFlagSet("pipeline", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD TSV input")
	taxonkitOut := fs.String("taxonkit-output", "taxonkit_input.tsv", "Output taxonkit input TSV")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Output taxdump directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Output marker FASTA directory")
	releaseDir := fs.String("releases-dir", "releases", "Release artifacts directory")
	taxonkitBin := fs.String("taxonkit-bin", "", "Path to taxonkit binary (default: search PATH)")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	noGzip := fs.Bool("no-gzip", false, "Disable gzip for marker FASTAs")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parser worker goroutines (<=0 defaults to GOMAXPROCS)")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	packageFlag := fs.Bool("package", false, "Create release zips, manifest, and checksums")
	skipManifest := fs.Bool("skip-manifest", false, "Skip manifest.json (only when --package)")
	skipChecksums := fs.Bool("skip-checksums", false, "Skip SHA256SUMS.txt (only when --package)")
	snapshot := fs.String("snapshot-id", "", "Snapshot ID suffix for releases (default: derive from input filename)")
	extractCurateProtocol := fs.String("extract-curate-protocol", extractCurationProtocolNone, "Extraction curation profile (none,bioscan-5m)")
	extractCurateReport := fs.String("extract-curate-report", "", "Optional extraction curation JSON report path")
	extractCurateAudit := fs.String("extract-curate-audit", "", "Optional extraction curation audit TSV path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}
	extractCfg := extractCurationConfig{
		Protocol:   *extractCurateProtocol,
		ReportPath: *extractCurateReport,
		AuditPath:  *extractCurateAudit,
	}.normalized()
	if err := extractCfg.validate(); err != nil {
		fatalf("invalid extraction curation config: %v", err)
	}

	snap := *snapshot
	if snap == "" {
		snap = snapshotID(*input)
	}

	totalRows := -1
	if *progressOn {
		count, err := countLines(*input)
		if err != nil {
			fatalf("count rows failed: %v", err)
		}
		if count > 0 {
			totalRows = count - 1
		}
	}

	reportEvery := 0
	if *progressOn {
		reportEvery = 1
	}

	if err := pipeline(*input, *taxonkitOut, *taxdumpDir, *markerDir, *releaseDir, *taxonkitBin, reportEvery, totalRows, *workers, !*noGzip, *force, *packageFlag, *skipManifest, *skipChecksums, snap, extractCfg); err != nil {
		fatalf("pipeline failed: %v", err)
	}
}

func pipeline(input, taxonkitOut, taxdumpDir, markerDir, releaseDir, taxonkitBin string, reportEvery, totalRows, workers int, gzipOut, force, doPackage, skipManifest, skipChecksums bool, snapshot string, extractCfg extractCurationConfig) error {
	logf("Extract taxonomy -> %s", taxonkitOut)
	if fileExists(taxonkitOut) && !force {
		logf("taxonkit TSV exists, skipping (use --force to overwrite): %s", taxonkitOut)
	} else {
		if _, err := buildTaxonkit(input, taxonkitOut, reportEvery, totalRows, extractCfg); err != nil {
			return fmt.Errorf("build taxonkit TSV: %w", err)
		}
	}

	logf("Build taxdump -> %s", taxdumpDir)
	if err := runTaxonkitCreate(taxonkitBin, taxonkitOut, taxdumpDir, force); err != nil {
		return fmt.Errorf("taxonkit create-taxdump: %w", err)
	}

	logf("Build marker FASTAs -> %s", markerDir)
	if outputsExist(markerDir) && !force {
		logf("marker FASTAs exist, skipping (use --force to overwrite): %s", markerDir)
	} else {
		if err := os.MkdirAll(markerDir, 0o755); err != nil {
			return fmt.Errorf("create marker output dir: %w", err)
		}
		if err := buildMarkerFastas(input, markerDir, gzipOut, reportEvery, totalRows, workers); err != nil {
			return fmt.Errorf("build markers: %w", err)
		}
	}

	if !doPackage {
		return nil
	}

	cfg := packageConfig{
		TaxdumpDir:    taxdumpDir,
		MarkerDir:     markerDir,
		TaxonkitOut:   taxonkitOut,
		ReleaseDir:    releaseDir,
		Snapshot:      snapshot,
		Force:         force,
		SkipManifest:  skipManifest,
		SkipChecksums: skipChecksums,
		MoveInputs:    true,
	}
	return packageRelease(cfg)
}

func runTaxonkitCreate(bin, input, outputDir string, force bool) error {
	taxonkit := bin
	if taxonkit == "" {
		if p, err := exec.LookPath("taxonkit"); err == nil {
			taxonkit = p
		} else if p, err := exec.LookPath("taxonkit.exe"); err == nil {
			taxonkit = p
		} else {
			return errors.New("taxonkit not found in PATH (set --taxonkit-bin)")
		}
	}

	if !force && fileExists(filepath.Join(outputDir, "nodes.dmp")) && fileExists(filepath.Join(outputDir, "names.dmp")) && fileExists(filepath.Join(outputDir, "taxid.map")) {
		logf("taxdump exists, skipping (use --force to overwrite): %s", outputDir)
		return nil
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create taxdump dir: %w", err)
	}

	cmd := exec.Command(taxonkit, "create-taxdump", input, "-A", "10", "--null", "None,NULL,NA", "-O", outputDir, "--force")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func packageMarkerPath(markerDir, releaseDir, snapshot string) string {
	suffix := ""
	if snapshot != "" {
		suffix = "." + safeTag(snapshot)
	}
	markerName := filepath.Base(markerDir) + suffix + ".tar.gz"
	return filepath.Join(releaseDir, markerName)
}

func packageTaxdumpArchivePath(taxdumpDir, releaseDir, snapshot string) string {
	suffix := ""
	if snapshot != "" {
		suffix = "." + safeTag(snapshot)
	}
	base := filepath.Base(taxdumpDir)
	return filepath.Join(releaseDir, base+suffix+".tar.gz")
}

func packageTaxonkitPath(taxonkitOut, releaseDir, snapshot string) string {
	base := filepath.Base(taxonkitOut)
	if snapshot != "" {
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		base = name + "." + safeTag(snapshot) + ext
	}
	return filepath.Join(releaseDir, base)
}

func packageTaxonkitGzipPath(taxonkitOut, releaseDir, snapshot string) string {
	base := filepath.Base(taxonkitOut)
	if snapshot != "" {
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		base = name + "." + safeTag(snapshot) + ext
	}
	if !strings.HasSuffix(base, ".gz") {
		base += ".gz"
	}
	return filepath.Join(releaseDir, base)
}

func packageTaxonkitGzip(src, dest string, force bool) error {
	if filepath.Clean(src) == filepath.Clean(dest) {
		logf("taxonkit gzip already in release dir: %s", dest)
		return nil
	}
	if fileExists(dest) && !force {
		logf("taxonkit gzip exists, skipping (use --force to overwrite): %s", dest)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create release dir: %w", err)
	}
	if strings.HasSuffix(src, ".gz") {
		return copyFile(src, dest)
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

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return nil
}

func packageDirGzip(srcDir, destTarGz string, force bool) error {
	if fileExists(destTarGz) && !force {
		logf("archive exists, skipping (use --force to overwrite): %s", destTarGz)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destTarGz), 0o755); err != nil {
		return fmt.Errorf("create releases dir: %w", err)
	}

	out, err := os.Create(destTarGz)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	gzw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	tw := tar.NewWriter(gzw)

	base := filepath.Base(srcDir)
	if err := filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join(base, rel))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, in)
		_ = in.Close()
		return err
	}); err != nil {
		_ = tw.Close()
		_ = gzw.Close()
		return err
	}

	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		return err
	}
	if err := gzw.Close(); err != nil {
		return err
	}
	return nil
}

func writeChecksums(releaseDir, outputFile string, force bool) error {
	if fileExists(outputFile) && !force {
		logf("checksums exist, skipping (use --force to overwrite): %s", outputFile)
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
		sum, err := sha256File(f)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s  %s\n", sum, filepath.Base(f)); err != nil {
			return err
		}
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeManifest(path, taxdumpDir, markerDir, snapshot string, force bool) error {
	if fileExists(path) && !force {
		logf("manifest exists, skipping (use --force to overwrite): %s", path)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	commit := "unknown"
	if c, err := gitCommitHash(); err == nil && c != "" {
		commit = c
	}

	nodes, err := countLines(filepath.Join(taxdumpDir, "nodes.dmp"))
	if err != nil {
		return err
	}
	names, err := countLines(filepath.Join(taxdumpDir, "names.dmp"))
	if err != nil {
		return err
	}
	taxid, err := countLines(filepath.Join(taxdumpDir, "taxid.map"))
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

	data := fmt.Sprintf(`{
  "snapshot_id": "%s",
  "commit_hash": "%s",
  "counts": {
    "nodes": %d,
    "names": %d,
    "taxid_map": %d,
    "marker_fasta_files": %d,
    "marker_fasta_sequences": %d
  }
}
`, snapshot, commit, nodes, names, taxid, len(markerFiles), markerSeqs)

	return os.WriteFile(path, []byte(data), 0o644)
}

func gitCommitHash() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
		rc, err := openInput(p)
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

func safeTag(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[boldkit] "+format+"\n", args...)
}
