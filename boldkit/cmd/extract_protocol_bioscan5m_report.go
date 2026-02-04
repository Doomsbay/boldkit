package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	bioscanRulesetVersion = "bioscan-5m.v1"

	rulePlaceholderNormalize       = "placeholder_normalize"
	ruleSubfamilyFill              = "subfamily_fill_from_family_tribe"
	ruleEpithetOnlyFix             = "species_epithet_only_fix"
	ruleGenusFromResolved          = "genus_from_resolved_species"
	ruleGenusInferred              = "genus_inferred_from_species"
	ruleBinCanonicalAdopt          = "bin_canonical_species_adopt"
	ruleGenusSpeciesMismatchDemote = "genus_species_mismatch_demote"
	ruleOpenToBinProvisional       = "open_or_empty_to_bin_provisional"
	ruleProvisionalDroppedNoBin    = "provisional_dropped_missing_bin"
)

type bioscanCurationStats struct {
	RowsTotal                  int `json:"rows_total"`
	RowsChanged                int `json:"rows_changed"`
	PlaceholderNormalize       int `json:"placeholder_normalize"`
	SubfamilyFilled            int `json:"subfamily_fill_from_family_tribe"`
	EpithetOnlyFixed           int `json:"species_epithet_only_fix"`
	GenusFromResolved          int `json:"genus_from_resolved_species"`
	GenusInferred              int `json:"genus_inferred_from_species"`
	BinCanonicalAdopted        int `json:"bin_canonical_species_adopt"`
	GenusSpeciesMismatchDemote int `json:"genus_species_mismatch_demote"`
	OpenToBinProvisional       int `json:"open_or_empty_to_bin_provisional"`
	ProvisionalDroppedNoBin    int `json:"provisional_dropped_missing_bin"`
}

func (s *bioscanCurationStats) addRules(ruleSet map[string]struct{}) {
	for rule := range ruleSet {
		switch rule {
		case rulePlaceholderNormalize:
			s.PlaceholderNormalize++
		case ruleSubfamilyFill:
			s.SubfamilyFilled++
		case ruleEpithetOnlyFix:
			s.EpithetOnlyFixed++
		case ruleGenusFromResolved:
			s.GenusFromResolved++
		case ruleGenusInferred:
			s.GenusInferred++
		case ruleBinCanonicalAdopt:
			s.BinCanonicalAdopted++
		case ruleGenusSpeciesMismatchDemote:
			s.GenusSpeciesMismatchDemote++
		case ruleOpenToBinProvisional:
			s.OpenToBinProvisional++
		case ruleProvisionalDroppedNoBin:
			s.ProvisionalDroppedNoBin++
		}
	}
}

type bioscanCurationBinSummary struct {
	Observed   int `json:"observed"`
	Canonical  int `json:"canonical"`
	Conflicted int `json:"conflicted"`
}

type bioscanCurationReport struct {
	Protocol       string                    `json:"protocol"`
	RulesetVersion string                    `json:"ruleset_version"`
	InputPath      string                    `json:"input_path"`
	AuditPath      string                    `json:"audit_path,omitempty"`
	BinSummary     bioscanCurationBinSummary `json:"bin_summary"`
	Stats          bioscanCurationStats      `json:"stats"`
}

func (c *bioscan5MCurator) openAudit() error {
	if c.cfg.AuditPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.AuditPath), 0o755); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	f, err := os.Create(c.cfg.AuditPath)
	if err != nil {
		return fmt.Errorf("create audit file: %w", err)
	}
	c.auditFile = f
	c.auditWriter = bufio.NewWriterSize(f, writerBufferSize)
	if _, err := c.auditWriter.WriteString("processid\tbin_uri\tgenus_before\tspecies_before\tsubfamily_before\tgenus_after\tspecies_after\tsubfamily_after\trules\n"); err != nil {
		return fmt.Errorf("write audit header: %w", err)
	}
	return nil
}

func (c *bioscan5MCurator) closeAudit() error {
	if c.auditWriter != nil {
		if err := c.auditWriter.Flush(); err != nil {
			return fmt.Errorf("flush audit: %w", err)
		}
		c.auditWriter = nil
	}
	if c.auditFile != nil {
		if err := c.auditFile.Close(); err != nil {
			return fmt.Errorf("close audit: %w", err)
		}
		c.auditFile = nil
	}
	return nil
}

func (c *bioscan5MCurator) writeAuditRow(before, after extractTaxonRecord, ruleSet map[string]struct{}, changed bool) error {
	if c.auditWriter == nil || !changed {
		return nil
	}
	rules := sortedRuleSet(ruleSet)
	line := strings.Join([]string{
		auditField(after.ProcessID),
		auditField(after.BinURI),
		auditField(before.Genus),
		auditField(before.Species),
		auditField(before.Subfamily),
		auditField(after.Genus),
		auditField(after.Species),
		auditField(after.Subfamily),
		strings.Join(rules, ","),
	}, "\t")
	if _, err := c.auditWriter.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("write audit row: %w", err)
	}
	return nil
}

func (c *bioscan5MCurator) writeReport() error {
	if c.cfg.ReportPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.ReportPath), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	f, err := os.Create(c.cfg.ReportPath)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	report := bioscanCurationReport{
		Protocol:       extractCurationProtocolBioscan5M,
		RulesetVersion: bioscanRulesetVersion,
		InputPath:      c.inputPath,
		AuditPath:      c.cfg.AuditPath,
		BinSummary: bioscanCurationBinSummary{
			Observed:   c.binsObserved,
			Canonical:  c.binsCanonical,
			Conflicted: c.binsConflicted,
		},
		Stats: c.stats,
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	logf("extract (%s): report -> %s", extractCurationProtocolBioscan5M, c.cfg.ReportPath)
	return nil
}

func sortedRuleSet(ruleSet map[string]struct{}) []string {
	if len(ruleSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(ruleSet))
	for rule := range ruleSet {
		out = append(out, rule)
	}
	sort.Strings(out)
	return out
}

func auditField(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}
