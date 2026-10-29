package taxonomy

import "strings"

type NameNode struct {
	Key  string
	Name string
}

type NameDisambiguator struct {
	nameRank    map[string]string
	keyParent   map[string]string
	keyDisplay  map[string]string
	disambigCnt int
}

func NewNameDisambiguator() *NameDisambiguator {
	return &NameDisambiguator{
		nameRank:   make(map[string]string),
		keyParent:  make(map[string]string),
		keyDisplay: make(map[string]string),
	}
}

func (d *NameDisambiguator) Resolve(name, rank, parentKey, parentName string) NameNode {
	nameRankKey := strings.ToLower(name) + "|" + rank
	if existingKey, ok := d.nameRank[nameRankKey]; ok {
		if d.keyParent[existingKey] == parentKey {
			return NameNode{Key: existingKey, Name: d.keyDisplay[existingKey]}
		}
		return d.disambiguate(name, rank, parentKey, parentName, 0)
	}
	key := name + "|" + rank
	d.nameRank[nameRankKey] = key
	d.keyParent[key] = parentKey
	d.keyDisplay[key] = name
	return NameNode{Key: key, Name: name}
}

func (d *NameDisambiguator) disambiguate(name, rank, parentKey, parentName string, depth int) NameNode {
	if depth > 50 {
		return NameNode{Key: name + "|" + rank, Name: name}
	}
	disambigName := parentName + "_" + name
	nameRankKey := strings.ToLower(disambigName) + "|" + rank
	if existingKey, ok := d.nameRank[nameRankKey]; ok {
		if d.keyParent[existingKey] == parentKey {
			return NameNode{Key: existingKey, Name: d.keyDisplay[existingKey]}
		}
		return d.disambiguate(disambigName, rank, parentKey, parentName, depth+1)
	}
	key := disambigName + "|" + rank
	d.nameRank[nameRankKey] = key
	d.keyParent[key] = parentKey
	d.keyDisplay[key] = disambigName
	d.disambigCnt++
	return NameNode{Key: key, Name: disambigName}
}

func (d *NameDisambiguator) DisambiguatedCount() int {
	return d.disambigCnt
}
