package cmd

import (
	"strings"
	"unicode"
)

type bioscanSpeciesKind string

const (
	bioscanSpeciesEmpty    bioscanSpeciesKind = "empty"
	bioscanSpeciesResolved bioscanSpeciesKind = "resolved"
	bioscanSpeciesOpen     bioscanSpeciesKind = "open"
)

type bioscanSpeciesInfo struct {
	Kind       bioscanSpeciesKind
	Normalized string
	Canonical  string
	Genus      string
	Epithet    string
}

var bioscanPlaceholderTokens = map[string]struct{}{
	"":             {},
	"-":            {},
	"n/a":          {},
	"na":           {},
	"none":         {},
	"null":         {},
	"unclassified": {},
	"undetermined": {},
	"unidentified": {},
	"unknown":      {},
}

var bioscanOpenNomenclatureTokens = map[string]struct{}{
	"aff":         {},
	"cf":          {},
	"complex":     {},
	"group":       {},
	"indet":       {},
	"nr":          {},
	"sp":          {},
	"spp":         {},
	"species":     {},
	"undescribed": {},
	"unknown":     {},
}

func bioscanNormalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	value = strings.Join(parts, " ")
	if _, ok := bioscanPlaceholderTokens[strings.ToLower(value)]; ok {
		return ""
	}
	return value
}

func bioscanNormalizeToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	token = strings.Trim(token, ".,;:()[]{}")
	return token
}

func bioscanIsOpenMarker(token string) bool {
	_, ok := bioscanOpenNomenclatureTokens[bioscanNormalizeToken(token)]
	return ok
}

func bioscanIsGenusToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	firstRune := true
	for _, r := range token {
		if firstRune {
			firstRune = false
			if !unicode.IsLetter(r) || !unicode.IsUpper(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && r != '-' {
			return false
		}
	}
	return !firstRune
}

func bioscanIsEpithetToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	for _, r := range token {
		if unicode.IsLower(r) || r == '-' {
			continue
		}
		return false
	}
	return true
}

func bioscanParseSpecies(species string) bioscanSpeciesInfo {
	norm := bioscanNormalizeLabel(species)
	if norm == "" {
		return bioscanSpeciesInfo{Kind: bioscanSpeciesEmpty}
	}

	parts := strings.Fields(norm)
	if len(parts) < 2 {
		return bioscanSpeciesInfo{
			Kind:       bioscanSpeciesOpen,
			Normalized: norm,
		}
	}

	if bioscanIsOpenMarker(parts[0]) || bioscanIsOpenMarker(parts[1]) {
		return bioscanSpeciesInfo{
			Kind:       bioscanSpeciesOpen,
			Normalized: norm,
		}
	}
	for _, part := range parts[2:] {
		if bioscanIsOpenMarker(part) {
			return bioscanSpeciesInfo{
				Kind:       bioscanSpeciesOpen,
				Normalized: norm,
			}
		}
	}

	genus := parts[0]
	epithet := strings.ToLower(parts[1])
	if !bioscanIsGenusToken(genus) || !bioscanIsEpithetToken(epithet) {
		return bioscanSpeciesInfo{
			Kind:       bioscanSpeciesOpen,
			Normalized: norm,
		}
	}

	return bioscanSpeciesInfo{
		Kind:       bioscanSpeciesResolved,
		Normalized: norm,
		Canonical:  genus + " " + epithet,
		Genus:      genus,
		Epithet:    epithet,
	}
}

func bioscanInferGenus(species string) string {
	species = bioscanNormalizeLabel(species)
	if species == "" {
		return ""
	}
	parts := strings.Fields(species)
	if len(parts) == 0 {
		return ""
	}
	head := parts[0]
	if bioscanIsOpenMarker(head) {
		return ""
	}
	if bioscanIsGenusToken(head) {
		return head
	}
	return ""
}

func bioscanProvisionalSpecies(genus, binURI string) string {
	genus = bioscanNormalizeLabel(genus)
	binURI = bioscanNormalizeLabel(binURI)
	if genus == "" || binURI == "" {
		return ""
	}
	return genus + " sp. " + binURI
}

type bioscanBinSpeciesResolver struct {
	counts map[string]map[string]int
}

type bioscanBinResolution struct {
	Canonical string
	Accepted  bool
	Conflict  bool
}

func newBioscanBinSpeciesResolver() *bioscanBinSpeciesResolver {
	return &bioscanBinSpeciesResolver{
		counts: make(map[string]map[string]int),
	}
}

func (r *bioscanBinSpeciesResolver) Observe(binURI, genus, species string) {
	if r == nil {
		return
	}
	bin := bioscanNormalizeLabel(binURI)
	if bin == "" {
		return
	}
	info := bioscanParseSpecies(species)
	if info.Kind != bioscanSpeciesResolved || info.Canonical == "" {
		return
	}

	genus = bioscanNormalizeLabel(genus)
	if genus != "" && !strings.EqualFold(genus, info.Genus) {
		return
	}

	bySpecies, ok := r.counts[bin]
	if !ok {
		bySpecies = make(map[string]int)
		r.counts[bin] = bySpecies
	}
	bySpecies[info.Canonical]++
}

func (r *bioscanBinSpeciesResolver) Resolve(binURI string) bioscanBinResolution {
	if r == nil {
		return bioscanBinResolution{}
	}
	bin := bioscanNormalizeLabel(binURI)
	if bin == "" {
		return bioscanBinResolution{}
	}

	bySpecies, ok := r.counts[bin]
	if !ok || len(bySpecies) == 0 {
		return bioscanBinResolution{}
	}

	if len(bySpecies) == 1 {
		for species := range bySpecies {
			return bioscanBinResolution{
				Canonical: species,
				Accepted:  true,
			}
		}
	}

	best := ""
	bestCount := -1
	second := -1
	total := 0
	for species, count := range bySpecies {
		total += count
		if count > bestCount || (count == bestCount && (best == "" || strings.Compare(species, best) < 0)) {
			second = bestCount
			best = species
			bestCount = count
			continue
		}
		if count > second {
			second = count
		}
	}

	// Accept only when there is a strict majority and a unique top species.
	if best != "" && bestCount > second && bestCount*2 > total {
		return bioscanBinResolution{
			Canonical: best,
			Accepted:  true,
		}
	}
	return bioscanBinResolution{
		Conflict: true,
	}
}
