package cmd

import "testing"

func TestBioscanNormalizeLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim and collapse", input: "  Panthera   leo  ", want: "Panthera leo"},
		{name: "none placeholder", input: "None", want: ""},
		{name: "na placeholder", input: "NA", want: ""},
		{name: "unknown placeholder", input: "unknown", want: ""},
		{name: "valid value", input: "Arthropoda", want: "Arthropoda"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := bioscanNormalizeLabel(tc.input)
			if got != tc.want {
				t.Fatalf("bioscanNormalizeLabel(%q)=%q want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBioscanParseSpecies(t *testing.T) {
	tests := []struct {
		name    string
		species string
		kind    bioscanSpeciesKind
		want    string
	}{
		{name: "resolved binomial", species: "Homo sapiens", kind: bioscanSpeciesResolved, want: "Homo sapiens"},
		{name: "resolved binomial mixed case epithet", species: "Homo Sapiens", kind: bioscanSpeciesResolved, want: "Homo sapiens"},
		{name: "open nomenclature sp", species: "Homo sp. BOLD:AAA0001", kind: bioscanSpeciesOpen, want: ""},
		{name: "open nomenclature cf", species: "Homo cf. sapiens", kind: bioscanSpeciesOpen, want: ""},
		{name: "placeholder", species: "None", kind: bioscanSpeciesEmpty, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := bioscanParseSpecies(tc.species)
			if info.Kind != tc.kind {
				t.Fatalf("bioscanParseSpecies(%q).Kind=%s want %s", tc.species, info.Kind, tc.kind)
			}
			if info.Canonical != tc.want {
				t.Fatalf("bioscanParseSpecies(%q).Canonical=%q want %q", tc.species, info.Canonical, tc.want)
			}
		})
	}
}

func TestBioscanProvisionalSpeciesNoProcessIDFallback(t *testing.T) {
	if got := bioscanProvisionalSpecies("Canis", "BOLD:AAA1111"); got != "Canis sp. BOLD:AAA1111" {
		t.Fatalf("bioscanProvisionalSpecies()=%q want %q", got, "Canis sp. BOLD:AAA1111")
	}
	if got := bioscanProvisionalSpecies("Canis", ""); got != "" {
		t.Fatalf("bioscanProvisionalSpecies()=%q want empty", got)
	}
}

func TestBioscanInferGenus(t *testing.T) {
	tests := []struct {
		species string
		want    string
	}{
		{species: "Homo sp. BOLD:AAA0001", want: "Homo"},
		{species: "Homo sapiens", want: "Homo"},
		{species: "cf. sapiens", want: ""},
		{species: "", want: ""},
	}
	for _, tc := range tests {
		got := bioscanInferGenus(tc.species)
		if got != tc.want {
			t.Fatalf("bioscanInferGenus(%q)=%q want %q", tc.species, got, tc.want)
		}
	}
}

func TestBioscanBinSpeciesResolverCanonical(t *testing.T) {
	resolver := newBioscanBinSpeciesResolver()

	resolver.Observe("BOLD:AAA1111", "Homo", "Homo sapiens")
	resolver.Observe("BOLD:AAA1111", "Homo", "Homo sapiens")
	resolver.Observe("BOLD:AAA1111", "Homo", "Homo erectus")

	got, ok := resolver.Canonical("BOLD:AAA1111")
	if !ok {
		t.Fatalf("resolver.Canonical() returned !ok")
	}
	if got != "Homo sapiens" {
		t.Fatalf("resolver.Canonical()=%q want %q", got, "Homo sapiens")
	}
}

func TestBioscanBinSpeciesResolverTieBreakLexical(t *testing.T) {
	resolver := newBioscanBinSpeciesResolver()

	resolver.Observe("BOLD:TIE0001", "Panthera", "Panthera leo")
	resolver.Observe("BOLD:TIE0001", "Panthera", "Panthera onca")

	got, ok := resolver.Canonical("BOLD:TIE0001")
	if !ok {
		t.Fatalf("resolver.Canonical() returned !ok")
	}
	if got != "Panthera leo" {
		t.Fatalf("resolver.Canonical()=%q want %q", got, "Panthera leo")
	}
}

func TestBioscanBinSpeciesResolverIgnoresUnresolvedAndMismatch(t *testing.T) {
	resolver := newBioscanBinSpeciesResolver()

	resolver.Observe("BOLD:BAD0001", "Homo", "Homo sp. BOLD:BAD0001")
	resolver.Observe("BOLD:BAD0001", "Homo", "None")
	resolver.Observe("BOLD:BAD0001", "Canis", "Homo sapiens")

	if got, ok := resolver.Canonical("BOLD:BAD0001"); ok || got != "" {
		t.Fatalf("resolver.Canonical()=%q,%v want empty,false", got, ok)
	}
}
