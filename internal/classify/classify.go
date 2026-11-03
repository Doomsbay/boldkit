package classify

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Doomsbay/BoldKit/internal/format"
	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/qc"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type Config struct {
	Input          string
	OutDir         string
	Classifiers    []string
	Ranks          []string
	TaxdumpDir     string
	TaxidMapPath   string
	QC             qc.Config
	QCOnly         bool
	Compress       bool
	Force          bool
	FormatProgress bool
}

func Classify(cfg Config) error {
	base := boldio.QCBaseName(cfg.Input)
	qcOut := filepath.Join(cfg.OutDir, "qc", base+".fasta")
	qcCfg := cfg.QC
	qcCfg.RequireRanks = cfg.Ranks
	qcCfg.TaxdumpDir = cfg.TaxdumpDir
	qcCfg.TaxidMapPath = cfg.TaxidMapPath
	qcCfg.OutputPath = qcOut

	util.Logf("QC -> %s", qcOut)
	if err := qc.RunQC(cfg.Input, qcCfg); err != nil {
		return fmt.Errorf("qc failed: %w", err)
	}

	if cfg.QCOnly {
		return nil
	}

	for _, classifier := range cfg.Classifiers {
		if classifier == "" {
			continue
		}
		name := strings.ToLower(classifier)
		outPath := filepath.Join(cfg.OutDir, name)
		formatCfg := format.FormatConfig{
			Classifiers:  []string{name},
			RequireRanks: cfg.Ranks,
			Input:        qcOut,
			OutDir:       outPath,
			TaxdumpDir:   cfg.TaxdumpDir,
			TaxidMapPath: cfg.TaxidMapPath,
			Progress:     cfg.FormatProgress,
		}
		util.Logf("Format %s -> %s", name, outPath)
		if err := format.FormatFasta(formatCfg); err != nil {
			return fmt.Errorf("format %s failed: %w", name, err)
		}

		if cfg.Compress {
			archive := filepath.Join(cfg.OutDir, name+".tar.gz")
			if err := boldio.PackageDirGzip(outPath, archive, cfg.Force); err != nil {
				return fmt.Errorf("compress %s failed: %w", name, err)
			}
		}
	}
	return nil
}
