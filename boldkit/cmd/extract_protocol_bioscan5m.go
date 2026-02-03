package cmd

import (
	"bufio"
	"fmt"
	"strings"
)

type bioscan5MCurator struct {
	cfg      extractCurationConfig
	resolver *bioscanBinSpeciesResolver
}

func newExtractBioscan5MCurator(cfg extractCurationConfig, inputPath string) (extractCurator, error) {
	c := &bioscan5MCurator{
		cfg:      cfg,
		resolver: newBioscanBinSpeciesResolver(),
	}
	if inputPath != "" {
		if err := c.prime(inputPath); err != nil {
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
	return nil
}

func (c *bioscan5MCurator) canonicalForBin(binURI string) (bioscanSpeciesInfo, bool) {
	canon, ok := c.resolver.Canonical(binURI)
	if !ok || canon == "" {
		return bioscanSpeciesInfo{}, false
	}
	info := bioscanParseSpecies(canon)
	if info.Kind != bioscanSpeciesResolved || info.Canonical == "" {
		return bioscanSpeciesInfo{}, false
	}
	return info, true
}

func (c *bioscan5MCurator) Curate(rec *extractTaxonRecord) error {
	if rec == nil {
		return nil
	}

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

	if rec.Family != "" && rec.Tribe != "" && rec.Subfamily == "" {
		rec.Subfamily = rec.Family + " subfam. incertae sedis"
	}

	if rec.Genus != "" && bioscanIsEpithetToken(rec.Species) {
		rec.Species = rec.Genus + " " + strings.ToLower(rec.Species)
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
			break
		}
		species = bioscanProvisionalSpecies(genus, rec.BinURI)

	case bioscanSpeciesOpen, bioscanSpeciesEmpty:
		if genus == "" {
			genus = bioscanInferGenus(speciesInfo.Normalized)
		}

		if hasBinCanonical && (genus == "" || strings.EqualFold(genus, binInfo.Genus)) {
			genus = binInfo.Genus
			species = binInfo.Canonical
			break
		}

		species = bioscanProvisionalSpecies(genus, rec.BinURI)
	default:
		species = bioscanProvisionalSpecies(genus, rec.BinURI)
	}

	rec.Genus = genus
	rec.Species = species
	return nil
}

func (c *bioscan5MCurator) Close() error {
	return nil
}
