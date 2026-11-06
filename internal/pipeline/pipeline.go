package pipeline

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Doomsbay/BoldKit/internal/extract"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/markers"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type Config struct {
	Input         string
	TaxonkitOut   string
	TaxdumpDir    string
	MarkerDir     string
	ReleaseDir    string
	TaxonkitBin   string
	ReportEvery   int
	TotalRows     int
	Workers       int
	GzipMarkers   bool
	Force         bool
	Package       bool
	SkipManifest  bool
	SkipChecksums bool
	Snapshot      string
	Extract       extract.CurationConfig
	Selected      [][]byte
	Version       string
}

func Run(cfg Config) error {
	util.Logf("Input format: %s", boldio.InputFormat(cfg.Input))
	util.Logf("Extract taxonomy -> %s", cfg.TaxonkitOut)
	if boldio.FileExists(cfg.TaxonkitOut) && !cfg.Force {
		util.Logf("taxonkit TSV exists, skipping (use --force to overwrite): %s", cfg.TaxonkitOut)
	} else {
		if _, err := extract.BuildTaxonkit(cfg.Input, cfg.TaxonkitOut, cfg.ReportEvery, cfg.TotalRows, cfg.Extract); err != nil {
			return fmt.Errorf("build taxonkit TSV: %w", err)
		}
	}

	util.Logf("Build taxdump -> %s", cfg.TaxdumpDir)
	if err := taxonkitCreate(cfg.TaxonkitBin, cfg.TaxonkitOut, cfg.TaxdumpDir, cfg.Force); err != nil {
		return fmt.Errorf("taxonkit create-taxdump: %w", err)
	}

	util.Logf("Build marker FASTAs -> %s", cfg.MarkerDir)
	selected, skipMarkers, err := markers.SelectMissing(cfg.MarkerDir, cfg.Selected, cfg.Force)
	if err != nil {
		return fmt.Errorf("check marker outputs: %w", err)
	}
	if skipMarkers {
		util.Logf("marker FASTAs exist, skipping (use --force to overwrite): %s", cfg.MarkerDir)
	} else {
		if err := markers.BuildMarkerFastas(cfg.Input, cfg.MarkerDir, cfg.GzipMarkers, cfg.ReportEvery, cfg.TotalRows, cfg.Workers, selected); err != nil {
			return fmt.Errorf("build markers: %w", err)
		}
	}

	if !cfg.Package {
		return nil
	}

	return PackageRelease(ReleaseConfig{
		TaxdumpDir:    cfg.TaxdumpDir,
		MarkerDir:     cfg.MarkerDir,
		TaxonkitOut:   cfg.TaxonkitOut,
		ReleaseDir:    cfg.ReleaseDir,
		Snapshot:      cfg.Snapshot,
		Version:       cfg.Version,
		Force:         cfg.Force,
		SkipManifest:  cfg.SkipManifest,
		SkipChecksums: cfg.SkipChecksums,
		MoveInputs:    true,
	})
}

func taxonkitCreate(bin, input, outputDir string, force bool) error {
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

	if !force && boldio.FileExists(filepath.Join(outputDir, "nodes.dmp")) && boldio.FileExists(filepath.Join(outputDir, "names.dmp")) && boldio.FileExists(filepath.Join(outputDir, "taxid.map")) {
		util.Logf("taxdump exists, skipping (use --force to overwrite): %s", outputDir)
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
