package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ProtocolNone      = "none"
	ProtocolBioscan5M = "bioscan-5m"
)

type CurationConfig struct {
	Protocol   string
	ReportPath string
	AuditPath  string
}

func (c CurationConfig) Normalized() CurationConfig {
	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	if c.Protocol == "" {
		c.Protocol = ProtocolNone
	}
	c.ReportPath = strings.TrimSpace(c.ReportPath)
	c.AuditPath = strings.TrimSpace(c.AuditPath)
	return c
}

func (c CurationConfig) Validate() error {
	switch c.Protocol {
	case ProtocolNone, ProtocolBioscan5M:
		// known profile
	default:
		return fmt.Errorf("unknown protocol %q (supported: %s,%s)", c.Protocol, ProtocolNone, ProtocolBioscan5M)
	}
	if c.ReportPath != "" && filepath.Clean(c.ReportPath) == "." {
		return fmt.Errorf("invalid report path %q", c.ReportPath)
	}
	if c.AuditPath != "" && filepath.Clean(c.AuditPath) == "." {
		return fmt.Errorf("invalid audit path %q", c.AuditPath)
	}
	return nil
}

func rejectCurationPathAliases(outputPath string, cfg CurationConfig) error {
	if cfg.ReportPath != "" {
		if samePath(cfg.ReportPath, outputPath) {
			return fmt.Errorf("curation report path %q refers to the output path", cfg.ReportPath)
		}
		if cfg.AuditPath != "" && samePath(cfg.ReportPath, cfg.AuditPath) {
			return fmt.Errorf("curation report path %q refers to the audit path", cfg.ReportPath)
		}
	}
	if cfg.AuditPath != "" && samePath(cfg.AuditPath, outputPath) {
		return fmt.Errorf("curation audit path %q refers to the output path", cfg.AuditPath)
	}
	return nil
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	aAbs, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bAbs, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if aAbs == bAbs {
		return true
	}
	aInfo, err := os.Stat(a)
	if err != nil {
		return false
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(aInfo, bInfo)
}

func (c CurationConfig) enabled() bool {
	return c.Protocol != ProtocolNone
}

type TaxonRecord struct {
	ProcessID string
	BinURI    string
	Kingdom   string
	Phylum    string
	Class     string
	Order     string
	Family    string
	Subfamily string
	Tribe     string
	Genus     string
	Species   string
}

type Curator interface {
	Curate(*TaxonRecord) error
	// Finalize flushes the report and audit to temp files; nothing is published yet.
	Finalize() error
	// Publish renames the finalized report and audit into place. Call only after
	// the extract output itself has been published.
	Publish() error
	// Discard abandons any unpublished report and audit, keeping previous files.
	Discard()
}

func NewCurator(cfg CurationConfig, inputPath string) (Curator, error) {
	switch cfg.Protocol {
	case ProtocolNone:
		return &NoopCurator{}, nil
	case ProtocolBioscan5M:
		return NewBioscan5MCurator(cfg, inputPath)
	default:
		return nil, fmt.Errorf("unsupported extraction curation protocol %q", cfg.Protocol)
	}
}

type NoopCurator struct{}

func (n *NoopCurator) Curate(*TaxonRecord) error {
	return nil
}

func (n *NoopCurator) Finalize() error {
	return nil
}

func (n *NoopCurator) Publish() error {
	return nil
}

func (n *NoopCurator) Discard() {}
