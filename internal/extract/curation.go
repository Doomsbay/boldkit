package extract

import (
	"fmt"
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
	Close() error
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

func (n *NoopCurator) Close() error {
	return nil
}
