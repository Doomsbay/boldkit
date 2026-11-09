package qc

import (
	"path/filepath"

	boldtaxonomy "github.com/Doomsbay/BoldKit/internal/taxonomy"
)

type RejectReason int

const (
	RejectNone RejectReason = iota
	RejectTaxID
	RejectRanks
	RejectGapped
	RejectShort
	RejectLong
	RejectN
	RejectAmbig
	RejectInvalid
)

type RecordFilter struct {
	cfg      Config
	taxidMap map[string]int
	dump     *boldtaxonomy.TaxDump
}

func NewRecordFilter(cfg Config) (*RecordFilter, error) {
	var taxidMap map[string]int
	var dump *boldtaxonomy.TaxDump
	if len(cfg.RequireRanks) > 0 || cfg.TaxidMapPath != "" {
		taxidPath := cfg.TaxidMapPath
		if taxidPath == "" {
			taxidPath = filepath.Join(cfg.TaxdumpDir, "taxid.map")
		}
		var err error
		taxidMap, err = boldtaxonomy.LoadTaxidMap(taxidPath)
		if err != nil {
			return nil, err
		}
	}
	if len(cfg.RequireRanks) > 0 {
		nodes, names := filepath.Join(cfg.TaxdumpDir, "nodes.dmp"), filepath.Join(cfg.TaxdumpDir, "names.dmp")
		var err error
		dump, err = boldtaxonomy.LoadTaxDump(nodes, names)
		if err != nil {
			return nil, err
		}
	}
	return &RecordFilter{cfg: cfg, taxidMap: taxidMap, dump: dump}, nil
}

func (f *RecordFilter) Accept(id string, seq []byte) ([]byte, SeqCounts, RejectReason) {
	clean, counts := CleanSequenceForPolicy(f.cfg.GapPolicy, seq)
	if f.taxidMap != nil {
		taxid, ok := f.taxidMap[id]
		if !ok {
			return clean, counts, RejectTaxID
		}
		if len(f.cfg.RequireRanks) > 0 && f.dump != nil {
			if !boldtaxonomy.HasAllRanks(f.dump.Lineage(taxid), f.cfg.RequireRanks) {
				return clean, counts, RejectRanks
			}
		}
	}
	if f.cfg.GapPolicy == GapPolicyDrop && counts.Gapped > 0 {
		return clean, counts, RejectGapped
	}
	if counts.Bases == 0 || (f.cfg.MinLen > 0 && counts.Bases < f.cfg.MinLen) {
		return clean, counts, RejectShort
	}
	if f.cfg.MaxLen > 0 && counts.Bases > f.cfg.MaxLen {
		return clean, counts, RejectLong
	}
	if f.cfg.MaxN >= 0 && counts.N > f.cfg.MaxN {
		return clean, counts, RejectN
	}
	if f.cfg.MaxAmbig >= 0 && counts.Ambig > f.cfg.MaxAmbig {
		return clean, counts, RejectAmbig
	}
	if counts.Invalid > f.cfg.MaxInvalid {
		return clean, counts, RejectInvalid
	}
	return clean, counts, RejectNone
}
