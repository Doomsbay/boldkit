package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/util"
)

type packageConfig struct {
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

func runPackage(args []string) {
	fs := flag.NewFlagSet("package", flag.ExitOnError)
	taxonkitOut := fs.String("taxonkit-output", "taxonkit_input.tsv", "Input taxonkit TSV to include")
	taxdumpDir := fs.String("taxdump-dir", "bold-taxdump", "Input taxdump directory")
	markerDir := fs.String("marker-dir", "marker_fastas", "Input marker FASTA directory")
	releaseDir := fs.String("releases-dir", "releases", "Release artifacts directory")
	snapshot := fs.String("snapshot-id", "", "Snapshot ID suffix for releases")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	skipManifest := fs.Bool("skip-manifest", false, "Skip manifest.json")
	skipChecksums := fs.Bool("skip-checksums", false, "Skip SHA256SUMS.txt")
	moveInputs := fs.Bool("move", true, "Move inputs into releases dir before packaging")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	snap := *snapshot
	if snap == "" {
		snap = snapshotID(*taxonkitOut)
	}

	cfg := packageConfig{
		TaxdumpDir:    *taxdumpDir,
		MarkerDir:     *markerDir,
		TaxonkitOut:   *taxonkitOut,
		ReleaseDir:    *releaseDir,
		Snapshot:      snap,
		Force:         *force,
		SkipManifest:  *skipManifest,
		SkipChecksums: *skipChecksums,
		MoveInputs:    *moveInputs,
	}

	if err := packageRelease(cfg); err != nil {
		fatalf("package failed: %v", err)
	}
}

func packageRelease(cfg packageConfig) error {
	util.Logf("Packaging release artifacts -> %s", cfg.ReleaseDir)
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
	if err := packageDirGzip(taxdumpDir, taxdumpArchive, cfg.Force); err != nil {
		return err
	}

	util.Logf("Package marker archive -> %s", markerZip)
	if err := packageDirGzip(markerDir, markerZip, cfg.Force); err != nil {
		return err
	}

	if !taxonkitIsGz {
		util.Logf("Package taxonkit input gzip -> %s", taxonkitGz)
		if err := packageTaxonkitGzip(taxonkitSource, taxonkitGz, cfg.Force); err != nil {
			return err
		}
	} else if taxonkitSource != taxonkitGz {
		util.Logf("Package taxonkit input gzip -> %s", taxonkitGz)
		if err := copyFile(taxonkitSource, taxonkitGz); err != nil {
			return err
		}
	}

	if !cfg.SkipManifest {
		manifestPath := filepath.Join(cfg.ReleaseDir, "manifest.json")
		util.Logf("Write manifest -> %s", manifestPath)
		if err := writeManifest(manifestPath, taxdumpDir, markerDir, cfg.Snapshot, cfg.Force); err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
	}

	if !cfg.SkipChecksums {
		sumPath := filepath.Join(cfg.ReleaseDir, "SHA256SUMS.txt")
		util.Logf("Write checksums -> %s", sumPath)
		if err := writeChecksums(cfg.ReleaseDir, sumPath, cfg.Force); err != nil {
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
	if pathExists(dest) {
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
		if err := copyDir(src, dest); err != nil {
			return err
		}
	} else {
		if err := copyFile(src, dest); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}

func copyDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", src)
	}
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("create dir %s: %w", dest, err)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks not supported in move fallback")
		}
		return copyFile(path, target)
	})
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
