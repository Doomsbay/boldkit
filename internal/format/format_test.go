package format

import (
	"bufio"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Doomsbay/BoldKit/internal/util"
)

func TestFormatFastaRejectsUnknownClassifierBeforeCreatingOutput(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "formatted")
	for _, classifiers := range [][]string{
		{"dnasketch"},
		{"blast", "dnasketch"},
		{"blast", ""},
	} {
		cfg := FormatConfig{
			Classifiers: classifiers,
			Input:       filepath.Join(dir, "input.fasta"),
			OutDir:      outDir,
		}
		if err := FormatFasta(cfg); err == nil {
			t.Fatalf("classifiers %v: expected error", classifiers)
		}
		if _, err := os.Stat(outDir); !os.IsNotExist(err) {
			t.Fatalf("classifiers %v: output directory was created", classifiers)
		}
	}
}

func TestValidateClassifiersAcceptsCaseVariants(t *testing.T) {
	for _, classifiers := range [][]string{
		{"BLAST"},
		{"Blast", "Kraken2"},
		{"sintax", "rdp", "idtaxa", "protax"},
	} {
		if err := ValidateClassifiers(classifiers); err != nil {
			t.Fatalf("classifiers %v: %v", classifiers, err)
		}
	}
}

func TestOpenFormatWritersClosesFilesOnSetupError(t *testing.T) {
	if _, err := os.ReadDir("/proc/self/fd"); err != nil {
		t.Skip("file descriptor inspection unavailable")
	}
	for _, tc := range []struct {
		classifier string
		fasta      string
		mapping    string
	}{
		{"blast", "blast.fasta", "blast_seqid2taxid.map"},
		{"rdp", "rdp_train_seqs.fasta", "rdp_taxonomy.txt"},
		{"idtaxa", "idtaxa_seqs.fasta", "idtaxa_lineage.tsv"},
		{"protax", "protax_seqs.fasta", "protax_seqid2tax.tsv"},
	} {
		t.Run(tc.classifier, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, tc.mapping), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := openFormatWriters(dir, []string{tc.classifier}); err == nil {
				t.Fatal("expected setup error")
			}
			entries, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
				if err == nil && target == filepath.Join(dir, tc.fasta) {
					t.Fatalf("FASTA file left open after setup failure: %s", target)
				}
			}
		})
	}
}

func TestCommitFormatWritersPropagatesFlushError(t *testing.T) {
	f, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("/dev/full unavailable: %v", err)
	}
	defer func() { _ = f.Close() }()
	buf := bufio.NewWriterSize(f, 64)
	if _, err := buf.WriteString(">P1\nACGT\n"); err != nil {
		t.Fatal(err)
	}
	writers := &formatWriters{blastFasta: writerHandle{w: buf, f: f}}
	if err := commitFormatWriters(writers); err == nil || !strings.Contains(err.Error(), "flush blast.fasta") {
		t.Fatalf("expected flush error, got %v", err)
	}
}

type taxonFixture struct {
	id     int
	parent int
	rank   string
	name   string
}

func writeTaxdumpFixture(t *testing.T, dir string, taxa []taxonFixture, idMap map[string]int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var nodes, names strings.Builder
	for _, tx := range taxa {
		id := strconv.Itoa(tx.id)
		nodes.WriteString(id + "\t|\t" + strconv.Itoa(tx.parent) + "\t|\t" + tx.rank + "\t|\n")
		names.WriteString(id + "\t|\t" + tx.name + "\t|\t\t|\tscientific name\t|\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "nodes.dmp"), []byte(nodes.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "names.dmp"), []byte(names.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var m strings.Builder
	for id, taxid := range idMap {
		m.WriteString(id + "\t" + strconv.Itoa(taxid) + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "taxid.map"), []byte(m.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

var protaxHomonymTaxa = []taxonFixture{
	{1, 1, "kingdom", "Animalia"},
	{2, 1, "phylum", "Arthropoda"},
	{3, 2, "class", "Insecta"},
	{4, 3, "order", "Lepidoptera"},
	{5, 4, "family", "FamilyA"},
	{6, 5, "genus", "Polyneura"},
	{7, 6, "species", "Polyneura alpha"},
	{11, 11, "kingdom", "Plantae"},
	{12, 11, "phylum", "AlgaePhylum"},
	{13, 12, "class", "AlgaeClass"},
	{14, 13, "order", "AlgaeOrder"},
	{15, 14, "family", "FamilyB"},
	{16, 15, "genus", "Polyneura"},
	{17, 16, "species", "Polyneura beta"},
}

func TestFormatProtaxCSVGolden(t *testing.T) {
	dir := t.TempDir()
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeTaxdumpFixture(t, taxdumpDir, protaxHomonymTaxa, map[string]int{"P1": 7, "P2": 17})
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nNNACGT-AANN\n>P2\nACGTAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "formatted")
	cfg := FormatConfig{
		Classifiers:  []string{"protax"},
		RequireRanks: util.SplitList("kingdom,phylum,class,order,family,genus,species"),
		Input:        input,
		OutDir:       outDir,
		TaxdumpDir:   taxdumpDir,
	}
	if err := FormatFasta(cfg); err != nil {
		t.Fatalf("FormatFasta: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "protax_refs.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := "phylum,class,order,family,genus,species,dna_barcode\n" +
		"Arthropoda,Insecta,Lepidoptera,FamilyA,Polyneura,Polyneura_alpha,NNACGT-AANN\n" +
		"AlgaePhylum,AlgaeClass,AlgaeOrder,FamilyB,FamilyB_Polyneura,Polyneura_beta,ACGTAA\n"
	if string(got) != want {
		t.Fatalf("protax refs csv:\ngot:  %q\nwant: %q", got, want)
	}

	fasta, err := os.ReadFile(filepath.Join(outDir, "protax_seqs.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fasta), ">P2\nACGTAA\n") {
		t.Fatalf("existing protax fasta changed: %q", fasta)
	}
}

func TestFormatProtaxCSVEmptyRankCells(t *testing.T) {
	dir := t.TempDir()
	taxa := append([]taxonFixture{}, protaxHomonymTaxa...)
	taxa = append(taxa,
		taxonFixture{25, 3, "family", "FamilyC"},
		taxonFixture{26, 25, "genus", "GenusC"},
		taxonFixture{27, 26, "species", "GenusC gamma"},
	)
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeTaxdumpFixture(t, taxdumpDir, taxa, map[string]int{"P3": 27})
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P3\nACGTA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "formatted")
	cfg := FormatConfig{
		Classifiers:  []string{"protax"},
		RequireRanks: util.SplitList("kingdom,phylum,class,family,genus,species"),
		Input:        input,
		OutDir:       outDir,
		TaxdumpDir:   taxdumpDir,
	}
	if err := FormatFasta(cfg); err != nil {
		t.Fatalf("FormatFasta: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "protax_refs.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := "phylum,class,order,family,genus,species,dna_barcode\n" +
		"Arthropoda,Insecta,,FamilyC,GenusC,GenusC_gamma,ACGTA\n"
	if string(got) != want {
		t.Fatalf("protax refs csv:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatProtaxCSVNamesUniqueAcrossRanks(t *testing.T) {
	dir := t.TempDir()
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeTaxdumpFixture(t, taxdumpDir, protaxHomonymTaxa, map[string]int{"P1": 7, "P1b": 7, "P2": 17})
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGT\n>P1b\nACGA\n>P2\nACGTAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "formatted")
	cfg := FormatConfig{
		Classifiers:  []string{"protax"},
		RequireRanks: util.SplitList("kingdom,phylum,class,order,family,genus,species"),
		Input:        input,
		OutDir:       outDir,
		TaxdumpDir:   taxdumpDir,
	}
	if err := FormatFasta(cfg); err != nil {
		t.Fatalf("FormatFasta: %v", err)
	}

	f, err := os.Open(filepath.Join(outDir, "protax_refs.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("expected header + 3 rows, got %d", len(records))
	}
	if strings.Join(records[0], ",") != "phylum,class,order,family,genus,species,dna_barcode" {
		t.Fatalf("unexpected header: %v", records[0])
	}
	nameColumn := make(map[string]int)
	for _, row := range records[1:] {
		if len(row) != 7 {
			t.Fatalf("expected 7 fields, got %d: %v", len(row), row)
		}
		for i := 0; i < 6; i++ {
			if row[i] == "" {
				continue
			}
			if col, ok := nameColumn[row[i]]; ok && col != i {
				t.Fatalf("name %q used at both columns %d and %d", row[i], col, i)
			}
			nameColumn[row[i]] = i
		}
	}
}

func TestFormatRdpDisambiguatesHomonyms(t *testing.T) {
	dir := t.TempDir()
	taxdumpDir := filepath.Join(dir, "taxdump")
	writeTaxdumpFixture(t, taxdumpDir, protaxHomonymTaxa, map[string]int{"P1": 7, "P2": 17})
	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1\nACGT\n>P2\nACGTAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "formatted")
	cfg := FormatConfig{
		Classifiers:  []string{"rdp"},
		RequireRanks: util.SplitList("kingdom,phylum,class,order,family,genus,species"),
		Input:        input,
		OutDir:       outDir,
		TaxdumpDir:   taxdumpDir,
	}
	if err := FormatFasta(cfg); err != nil {
		t.Fatalf("FormatFasta: %v", err)
	}

	fasta, err := os.ReadFile(filepath.Join(outDir, "rdp_train_seqs.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fasta), ";FamilyA;Polyneura;Polyneura_alpha") {
		t.Fatalf("animal lineage missing or renamed: %q", fasta)
	}
	if !strings.Contains(string(fasta), ";FamilyB;FamilyB_Polyneura;Polyneura_beta") {
		t.Fatalf("homonym genus was not disambiguated: %q", fasta)
	}

	taxonomy, err := os.ReadFile(filepath.Join(outDir, "rdp_taxonomy.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(taxonomy), "FamilyB_Polyneura") {
		t.Fatalf("disambiguated genus missing from taxonomy: %q", taxonomy)
	}
}

func TestFormatRdpKeepsLineageInHeaders(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "formatted")
	taxdumpDir := filepath.Join(dir, "taxdump")
	if err := os.MkdirAll(taxdumpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	taxa := []struct {
		id   int
		rank string
		name string
	}{
		{1, "kingdom", "Animalia"},
		{2, "phylum", "Chordata"},
		{3, "class", "Mammalia"},
		{4, "order", "Primates"},
		{5, "family", "Muridae"},
		{6, "genus", "Mus"},
		{7, "species", "Mus musculus"},
	}
	var nodes, names strings.Builder
	for i, tx := range taxa {
		parent := tx.id
		if i > 0 {
			parent = taxa[i-1].id
		}
		id := strconv.Itoa(tx.id)
		nodes.WriteString(id + "\t|\t" + strconv.Itoa(parent) + "\t|\t" + tx.rank + "\t|\n")
		names.WriteString(id + "\t|\t" + tx.name + "\t|\t\t|\tscientific name\t|\n")
	}
	if err := os.WriteFile(filepath.Join(taxdumpDir, "nodes.dmp"), []byte(nodes.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taxdumpDir, "names.dmp"), []byte(names.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taxdumpDir, "taxid.map"), []byte("P1\t7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := filepath.Join(dir, "input.fasta")
	if err := os.WriteFile(input, []byte(">P1 desc\nACGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := FormatConfig{
		Classifiers:  []string{"rdp"},
		RequireRanks: util.SplitList("kingdom,phylum,class,order,family,genus,species"),
		Input:        input,
		OutDir:       outDir,
		TaxdumpDir:   taxdumpDir,
	}
	if err := FormatFasta(cfg); err != nil {
		t.Fatalf("FormatFasta: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "rdp_train_seqs.fasta"))
	if err != nil {
		t.Fatal(err)
	}
	want := ">P1\tRoot;Animalia;Chordata;Mammalia;Primates;Muridae;Mus;Mus_musculus\nACGT\n"
	if string(got) != want {
		t.Fatalf("rdp train seqs:\ngot:  %q\nwant: %q", got, want)
	}

	taxonomy, err := os.ReadFile(filepath.Join(outDir, "rdp_taxonomy.txt"))
	if err != nil {
		t.Fatal(err)
	}
	taxLines := strings.Split(strings.TrimSpace(string(taxonomy)), "\n")
	if len(taxLines) != len(taxa)+1 {
		t.Fatalf("expected %d taxonomy lines, got %d: %q", len(taxa)+1, len(taxLines), taxonomy)
	}
	if taxLines[0] != "0*Root*-1*0*rootrank" {
		t.Fatalf("unexpected root taxonomy line: %q", taxLines[0])
	}
}
