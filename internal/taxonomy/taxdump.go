package taxonomy

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type TaxNode struct {
	Parent int
	Rank   string
	Name   string
}

type TaxDump struct {
	nodes map[int]TaxNode
	cache map[int]map[string]string
	alias map[string]string
}

func LoadTaxDump(nodesPath, namesPath string) (*TaxDump, error) {
	names, err := loadNames(namesPath)
	if err != nil {
		return nil, err
	}
	nodes, err := loadNodes(nodesPath, names)
	if err != nil {
		return nil, err
	}
	return &TaxDump{
		nodes: nodes,
		cache: make(map[int]map[string]string),
		alias: map[string]string{
			"superkingdom": "kingdom",
		},
	}, nil
}

func loadNames(path string) (map[int]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open names.dmp: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	names := make(map[int]string, 1<<20)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		fields := parseDmpLine(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		if fields[3] != "scientific name" {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if fields[1] == "" {
			continue
		}
		names[id] = fields[1]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan names.dmp: %w", err)
	}
	return names, nil
}

func loadNodes(path string, names map[int]string) (map[int]TaxNode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open nodes.dmp: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	nodes := make(map[int]TaxNode, 1<<20)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		fields := parseDmpLine(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		name := names[id]
		nodes[id] = TaxNode{
			Parent: parent,
			Rank:   fields[2],
			Name:   name,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan nodes.dmp: %w", err)
	}
	return nodes, nil
}

func parseDmpLine(line string) []string {
	raw := strings.Split(line, "|")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		field := strings.TrimSpace(part)
		if field != "" || len(out) > 0 {
			out = append(out, field)
		}
	}
	return out
}

func (t *TaxDump) Node(taxid int) (TaxNode, bool) {
	node, ok := t.nodes[taxid]
	return node, ok
}

func (t *TaxDump) Lineage(taxid int) map[string]string {
	if taxid <= 0 {
		return nil
	}
	if cached, ok := t.cache[taxid]; ok {
		return cached
	}
	lineage := make(map[string]string, 8)
	cur := taxid
	seen := 0
	for cur > 0 && seen < 64 {
		seen++
		node, ok := t.nodes[cur]
		if !ok {
			break
		}
		rank := node.Rank
		if alias, ok := t.alias[rank]; ok {
			rank = alias
		}
		if rank != "" && rank != "no rank" && node.Name != "" {
			if _, exists := lineage[rank]; !exists {
				lineage[rank] = node.Name
			}
		}
		if node.Parent == cur {
			break
		}
		cur = node.Parent
	}
	t.cache[taxid] = lineage
	return lineage
}
