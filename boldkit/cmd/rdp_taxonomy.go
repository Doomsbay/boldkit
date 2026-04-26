package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// rdpTaxonNode represents a node in the RDP taxonomy tree
type rdpTaxonNode struct {
	taxid     int    // assigned taxid
	name      string // canonical name (first-seen or disambiguated)
	rank      string // RDP rank label (domain, phylum, etc.)
	depth     int    // depth in tree (Root=0)
	parentKey string // key of parent node
}

// rdpTaxonomyBuilder manages construction of an RDP-compatible taxonomy tree
type rdpTaxonomyBuilder struct {
	nodes       map[string]*rdpTaxonNode // key -> node (key = "name|rank" or disambiguated)
	nameRank    map[string]string        // "(name_lower|rank)" -> node key
	taxids      map[string]int           // key -> assigned taxid
	nextTaxid   int                      // next taxid to assign
	ranks       []string                 // ordered rank list
	disambigCnt int                      // count of disambiguated names
}

// rdpRankAliases maps source ranks to RDP rank labels
var rdpRankAliases = map[string]string{
	"superkingdom": "domain",
	"kingdom":      "domain",
}

// newRdpTaxonomyBuilder creates a new taxonomy builder with the given rank order
func newRdpTaxonomyBuilder(ranks []string) *rdpTaxonomyBuilder {
	// Map ranks to RDP labels
	rdpRanks := make([]string, len(ranks))
	for i, r := range ranks {
		if alias, ok := rdpRankAliases[r]; ok {
			rdpRanks[i] = alias
		} else {
			rdpRanks[i] = r
		}
	}
	return &rdpTaxonomyBuilder{
		nodes:     make(map[string]*rdpTaxonNode),
		nameRank:  make(map[string]string),
		taxids:    make(map[string]int),
		nextTaxid: 1, // 0 reserved for Root
		ranks:     rdpRanks,
	}
}

// addLineage adds a lineage to the tree and returns the resolved node keys
// names should be in rank order (e.g., [domain_val, phylum_val, class_val, ...])
func (b *rdpTaxonomyBuilder) addLineage(names []string) []string {
	if len(names) == 0 {
		return nil
	}

	keys := make([]string, 0, len(names)+1)
	parentKey := "root"
	parentName := "Root"

	// Add Root
	if _, ok := b.nodes["root"]; !ok {
		b.nodes["root"] = &rdpTaxonNode{
			taxid:     0,
			name:      "Root",
			rank:      "rootrank",
			depth:     0,
			parentKey: "",
		}
		b.taxids["root"] = 0
	}
	keys = append(keys, "root")

	for i, name := range names {
		if i >= len(b.ranks) {
			break
		}
		rank := b.ranks[i]
		depth := i + 1

		// Handle empty names
		if name == "" || name == "-" {
			name = parentName + "_unclassified_" + rank
		}

		key := b.resolveName(name, rank, parentKey, depth)
		keys = append(keys, key)
		parentKey = key
		parentName = b.nodes[key].name
	}

	return keys
}

// resolveName resolves a name to a unique node key, handling conflicts
func (b *rdpTaxonomyBuilder) resolveName(name, rank, parentKey string, depth int) string {
	nameLower := strings.ToLower(name)
	nameRankKey := nameLower + "|" + rank

	// Check if (name_lower, rank) already exists
	if existingKey, ok := b.nameRank[nameRankKey]; ok {
		existing := b.nodes[existingKey]
		if existing.parentKey == parentKey {
			// Same parent: merge case variants, use first-seen casing
			return existingKey
		}
		// Different parent: disambiguate
		return b.disambiguate(name, rank, parentKey, depth)
	}

	// New node
	key := name + "|" + rank
	b.nameRank[nameRankKey] = key
	b.nodes[key] = &rdpTaxonNode{
		name:      name,
		rank:      rank,
		depth:     depth,
		parentKey: parentKey,
	}
	return key
}

// disambiguate handles same name under different parent by prefixing with parent name
func (b *rdpTaxonomyBuilder) disambiguate(name, rank, parentKey string, depth int) string {
	if depth > 50 {
		key := name + "|" + rank
		return key
	}
	parent := b.nodes[parentKey]
	disambigName := parent.name + "_" + name

	// Check if disambiguated name also conflicts
	nameLower := strings.ToLower(disambigName)
	nameRankKey := nameLower + "|" + rank

	if existingKey, ok := b.nameRank[nameRankKey]; ok {
		existing := b.nodes[existingKey]
		if existing.parentKey == parentKey {
			return existingKey
		}
		// Still conflicts, recurse with grandparent prefix
		return b.disambiguate(disambigName, rank, parentKey, depth+1)
	}

	// Create disambiguated node
	key := disambigName + "|" + rank
	b.nameRank[nameRankKey] = key
	b.nodes[key] = &rdpTaxonNode{
		name:      disambigName,
		rank:      rank,
		depth:     depth,
		parentKey: parentKey,
	}
	b.disambigCnt++
	return key
}

// assignTaxids assigns integer taxids to all nodes in breadth-first order
func (b *rdpTaxonomyBuilder) assignTaxids() {
	// Root already has taxid 0
	// Assign taxids by depth order
	byDepth := make(map[int][]string)
	for key, node := range b.nodes {
		if key == "root" {
			continue
		}
		byDepth[node.depth] = append(byDepth[node.depth], key)
	}

	// Sort depths
	depths := make([]int, 0, len(byDepth))
	for d := range byDepth {
		depths = append(depths, d)
	}
	sort.Ints(depths)

	// Assign taxids in depth order
	for _, d := range depths {
		keys := byDepth[d]
		sort.Strings(keys) // deterministic ordering
		for _, key := range keys {
			b.taxids[key] = b.nextTaxid
			b.nodes[key].taxid = b.nextTaxid
			b.nextTaxid++
		}
	}
}

// writeTaxonomyFile writes the taxonomy in RDP format
// Format: taxid*name*parent_taxid*depth*rank
func (b *rdpTaxonomyBuilder) writeTaxonomyFile(w io.Writer) error {
	// Assign taxids first
	b.assignTaxids()

	// Collect all nodes and sort by taxid
	type line struct {
		taxid int
		str   string
	}
	lines := make([]line, 0, len(b.nodes))

	for _, node := range b.nodes {
		parentTaxid := -1
		if node.parentKey != "" {
			if pid, ok := b.taxids[node.parentKey]; ok {
				parentTaxid = pid
			}
		}
		l := fmt.Sprintf("%d*%s*%d*%d*%s", node.taxid, node.name, parentTaxid, node.depth, node.rank)
		lines = append(lines, line{taxid: node.taxid, str: l})
	}

	// Sort by taxid
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].taxid < lines[j].taxid
	})

	// Write lines
	for _, l := range lines {
		if _, err := io.WriteString(w, l.str+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// getLineageString returns the semicolon-delimited lineage string for a sequence of node keys
func (b *rdpTaxonomyBuilder) getLineageString(keys []string) string {
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		if node, ok := b.nodes[key]; ok {
			names = append(names, node.name)
		}
	}
	return strings.Join(names, ";")
}

// disambiguatedCount returns the count of disambiguated names
func (b *rdpTaxonomyBuilder) disambiguatedCount() int {
	return b.disambigCnt
}
