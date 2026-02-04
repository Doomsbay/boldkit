package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type bioscan5MCurator struct {
	cfg            extractCurationConfig
	inputPath      string
	resolver       *bioscanBinSpeciesResolver
	binCanonical   map[string]bioscanSpeciesInfo
	binsObserved   int
	binsCanonical  int
	binsConflicted int
	stats          bioscanCurationStats
	auditFile      *os.File
	auditWriter    *bufio.Writer
}

func newExtractBioscan5MCurator(cfg extractCurationConfig, inputPath string) (extractCurator, error) {
	c := &bioscan5MCurator{
		cfg:          cfg,
		inputPath:    inputPath,
		resolver:     newBioscanBinSpeciesResolver(),
		binCanonical: make(map[string]bioscanSpeciesInfo),
	}
	if err := c.openAudit(); err != nil {
		return nil, err
	}
	if inputPath != "" {
		if err := c.prime(inputPath); err != nil {
			_ = c.closeAudit()
			return nil, err
		}
	}
	return c, nil
}

func (c *bioscan5MCurator) prime(inputPath string) error {
	in, err := openInput(inputPath)
	if err != nil {
		return fmt.Errorf("open input for bioscan prime: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	scanner := bufio.NewScanner(in)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read bioscan prime header: %w", err)
		}
		return fmt.Errorf("input TSV is empty")
	}

	header := strings.Split(scanner.Text(), "\t")
	idxBin := indexOf(header, "bin_uri")
	idxGenus := indexOf(header, "genus")
	idxSpecies := indexOf(header, "species")
	if idxBin < 0 || idxGenus < 0 || idxSpecies < 0 {
		return fmt.Errorf("required headers missing in input TSV")
	}

	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		binURI := bioscanNormalizeLabel(field(fields, idxBin))
		genus := bioscanNormalizeLabel(field(fields, idxGenus))
		species := bioscanNormalizeLabel(field(fields, idxSpecies))
		c.resolver.Observe(binURI, genus, species)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan bioscan prime input: %w", err)
	}
	c.buildBinDecisions()
	return nil
}

func (c *bioscan5MCurator) buildBinDecisions() {
	c.binsObserved = 0
	c.binsCanonical = 0
	c.binsConflicted = 0
	c.binCanonical = make(map[string]bioscanSpeciesInfo)
	for bin := range c.resolver.counts {
		c.binsObserved++
		resolution := c.resolver.Resolve(bin)
		if resolution.Accepted {
			info := bioscanParseSpecies(resolution.Canonical)
			if info.Kind == bioscanSpeciesResolved && info.Canonical != "" {
				c.binCanonical[bin] = info
				c.binsCanonical++
			}
			continue
		}
		if resolution.Conflict {
			c.binsConflicted++
		}
	}
}

func (c *bioscan5MCurator) canonicalForBin(binURI string) (bioscanSpeciesInfo, bool) {
	bin := bioscanNormalizeLabel(binURI)
	if bin == "" {
		return bioscanSpeciesInfo{}, false
	}
	info, ok := c.binCanonical[bin]
	if !ok {
		return bioscanSpeciesInfo{}, false
	}
	return info, true
}

func (c *bioscan5MCurator) Curate(rec *extractTaxonRecord) error {
	if rec == nil {
		return nil
	}
	c.stats.RowsTotal++
	original := *rec
	ruleSet := make(map[string]struct{})

	rec.Kingdom = bioscanNormalizeLabel(rec.Kingdom)
	rec.Phylum = bioscanNormalizeLabel(rec.Phylum)
	rec.Class = bioscanNormalizeLabel(rec.Class)
	rec.Order = bioscanNormalizeLabel(rec.Order)
	rec.Family = bioscanNormalizeLabel(rec.Family)
	rec.Subfamily = bioscanNormalizeLabel(rec.Subfamily)
	rec.Tribe = bioscanNormalizeLabel(rec.Tribe)
	rec.Genus = bioscanNormalizeLabel(rec.Genus)
	rec.Species = bioscanNormalizeLabel(rec.Species)
	rec.BinURI = bioscanNormalizeLabel(rec.BinURI)
	if rec.Kingdom != original.Kingdom || rec.Phylum != original.Phylum || rec.Class != original.Class ||
		rec.Order != original.Order || rec.Family != original.Family || rec.Subfamily != original.Subfamily ||
		rec.Tribe != original.Tribe || rec.Genus != original.Genus || rec.Species != original.Species ||
		rec.BinURI != original.BinURI {
		ruleSet[rulePlaceholderNormalize] = struct{}{}
	}

	if rec.Family != "" && rec.Tribe != "" && rec.Subfamily == "" {
		rec.Subfamily = rec.Family + " subfam. incertae sedis"
		ruleSet[ruleSubfamilyFill] = struct{}{}
	}

	if rec.Genus != "" && bioscanIsEpithetToken(rec.Species) {
		rec.Species = rec.Genus + " " + strings.ToLower(rec.Species)
		ruleSet[ruleEpithetOnlyFix] = struct{}{}
	}

	speciesInfo := bioscanParseSpecies(rec.Species)
	binInfo, hasBinCanonical := c.canonicalForBin(rec.BinURI)
	genus := rec.Genus
	species := rec.Species

	switch speciesInfo.Kind {
	case bioscanSpeciesResolved:
		if genus == "" {
			genus = speciesInfo.Genus
			species = speciesInfo.Canonical
			ruleSet[ruleGenusFromResolved] = struct{}{}
			break
		}

		if strings.EqualFold(genus, speciesInfo.Genus) {
			genus = speciesInfo.Genus
			species = speciesInfo.Canonical
			break
		}

		if hasBinCanonical && strings.EqualFold(genus, binInfo.Genus) {
			genus = binInfo.Genus
			species = binInfo.Canonical
			ruleSet[ruleBinCanonicalAdopt] = struct{}{}
			break
		}
		species = bioscanProvisionalSpecies(genus, rec.BinURI)
		ruleSet[ruleGenusSpeciesMismatchDemote] = struct{}{}

	case bioscanSpeciesOpen, bioscanSpeciesEmpty:
		if genus == "" {
			genus = bioscanInferGenus(speciesInfo.Normalized)
			if genus != "" {
				ruleSet[ruleGenusInferred] = struct{}{}
			}
		}

		if hasBinCanonical && (genus == "" || strings.EqualFold(genus, binInfo.Genus)) {
			genus = binInfo.Genus
			species = binInfo.Canonical
			ruleSet[ruleBinCanonicalAdopt] = struct{}{}
			break
		}

		species = bioscanProvisionalSpecies(genus, rec.BinURI)
		ruleSet[ruleOpenToBinProvisional] = struct{}{}
	default:
		species = bioscanProvisionalSpecies(genus, rec.BinURI)
		ruleSet[ruleOpenToBinProvisional] = struct{}{}
	}

	rec.Genus = genus
	rec.Species = species
	_, provisionalRule := ruleSet[ruleOpenToBinProvisional]
	_, mismatchRule := ruleSet[ruleGenusSpeciesMismatchDemote]
	if (provisionalRule || mismatchRule) && rec.Species == "" {
		ruleSet[ruleProvisionalDroppedNoBin] = struct{}{}
	}
	changed := original.Genus != rec.Genus || original.Species != rec.Species || original.Subfamily != rec.Subfamily ||
		original.Kingdom != rec.Kingdom || original.Phylum != rec.Phylum || original.Class != rec.Class ||
		original.Order != rec.Order || original.Family != rec.Family || original.Tribe != rec.Tribe || original.BinURI != rec.BinURI
	if changed {
		c.stats.RowsChanged++
	}
	c.stats.addRules(ruleSet)
	if err := c.writeAuditRow(original, *rec, ruleSet, changed); err != nil {
		return err
	}
	return nil
}

func (c *bioscan5MCurator) Close() error {
	logf("extract (%s): bins-observed=%d bins-canonical=%d bins-conflicted=%d", extractCurationProtocolBioscan5M, c.binsObserved, c.binsCanonical, c.binsConflicted)
	var firstErr error
	if err := c.writeReport(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := c.closeAudit(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
