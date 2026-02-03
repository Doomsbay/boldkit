package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	extractCurationProtocolNone      = "none"
	extractCurationProtocolBioscan5M = "bioscan-5m"
)

type extractCurationConfig struct {
	Protocol   string
	ReportPath string
	AuditPath  string
}

func (c extractCurationConfig) normalized() extractCurationConfig {
	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	if c.Protocol == "" {
		c.Protocol = extractCurationProtocolNone
	}
	c.ReportPath = strings.TrimSpace(c.ReportPath)
	c.AuditPath = strings.TrimSpace(c.AuditPath)
	return c
}

func (c extractCurationConfig) validate() error {
	switch c.Protocol {
	case extractCurationProtocolNone, extractCurationProtocolBioscan5M:
		// known profile
	default:
		return fmt.Errorf("unknown protocol %q (supported: %s,%s)", c.Protocol, extractCurationProtocolNone, extractCurationProtocolBioscan5M)
	}
	if c.ReportPath != "" && filepath.Clean(c.ReportPath) == "." {
		return fmt.Errorf("invalid report path %q", c.ReportPath)
	}
	if c.AuditPath != "" && filepath.Clean(c.AuditPath) == "." {
		return fmt.Errorf("invalid audit path %q", c.AuditPath)
	}
	return nil
}

func (c extractCurationConfig) enabled() bool {
	return c.Protocol != extractCurationProtocolNone
}

type extractTaxonRecord struct {
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

type extractCurator interface {
	Curate(*extractTaxonRecord) error
	Close() error
}

func newExtractCurator(cfg extractCurationConfig, inputPath string) (extractCurator, error) {
	switch cfg.Protocol {
	case extractCurationProtocolNone:
		return &noopExtractCurator{}, nil
	case extractCurationProtocolBioscan5M:
		return newExtractBioscan5MCurator(cfg, inputPath)
	default:
		return nil, fmt.Errorf("unsupported extraction curation protocol %q", cfg.Protocol)
	}
}

type noopExtractCurator struct{}

func (n *noopExtractCurator) Curate(*extractTaxonRecord) error {
	return nil
}

func (n *noopExtractCurator) Close() error {
	return nil
}
