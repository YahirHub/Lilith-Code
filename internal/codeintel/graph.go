package codeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Graph builds a compact file/package/symbol graph from imports, declarations
// and syntax references. Query matching seeds the graph, then adjacent nodes are
// expanded so a task-oriented request keeps the edges needed to explain flow.
func (m *Manager) Graph(ctx context.Context, query string, limit int) (RepositoryGraph, error) {
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	if _, err := m.EnsureFresh(ctx); err != nil && ctx.Err() != nil {
		return RepositoryGraph{}, err
	}
	idx := m.Snapshot()
	terms := semanticTerms(query)

	definitionsByQualified := map[string][]Symbol{}
	definitionsByName := map[string][]Symbol{}
	packageFiles := map[string][]string{}
	for _, path := range sortedFileKeys(idx.Files) {
		record := idx.Files[path]
		if record.PackagePath != "" {
			packageFiles[normalizeCanonical(record.PackagePath)] = append(packageFiles[normalizeCanonical(record.PackagePath)], path)
		}
		for _, symbol := range record.Symbols {
			if symbol.QualifiedName != "" {
				key := normalizeCanonical(symbol.QualifiedName)
				definitionsByQualified[key] = append(definitionsByQualified[key], symbol)
			}
			definitionsByName[strings.ToLower(symbol.Name)] = append(definitionsByName[strings.ToLower(symbol.Name)], symbol)
		}
	}

	nodes := map[string]GraphNode{}
	scores := map[string]int{}
	centrality := map[string]int{}
	edgeSeen := map[string]bool{}
	edges := make([]GraphEdge, 0)
	addNode := func(node GraphNode, score int) {
		if existing, ok := nodes[node.ID]; ok {
			if score > scores[node.ID] {
				scores[node.ID] = score
				existing.Weight = score
				nodes[node.ID] = existing
			}
			return
		}
		node.Weight = score
		nodes[node.ID] = node
		scores[node.ID] = score
	}
	addEdge := func(from, to, kind string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to + "\x00" + kind
		if edgeSeen[key] {
			return
		}
		edgeSeen[key] = true
		edges = append(edges, GraphEdge{From: from, To: to, Kind: kind})
		centrality[from]++
		centrality[to] += 2
	}

	for _, path := range sortedFileKeys(idx.Files) {
		record := idx.Files[path]
		if !taskAllowsPath(path, terms) {
			continue
		}
		fileID := "file:" + path
		fileScore := semanticScore(terms, recordSemanticValues(record)...)
		fileScore += pathRankingAdjustment(path, terms)
		if isTestPath(path) && terms["test"] == 0 {
			fileScore /= 3
		}
		if strings.TrimSpace(query) == "" {
			fileScore = maxInt(fileScore, 1)
		}
		addNode(GraphNode{ID: fileID, Kind: "file", Path: path, Name: filepath.Base(path), Language: record.Language}, fileScore)

		packageID := ""
		if record.PackagePath != "" {
			packageID = "package:" + record.PackagePath
			packageScore := semanticScore(terms, record.PackagePath, record.Package)
			addNode(GraphNode{ID: packageID, Kind: "package", Path: filepath.ToSlash(filepath.Dir(path)), Name: record.PackagePath, Language: record.Language}, packageScore)
			addEdge(packageID, fileID, "contains")
		}

		for _, imported := range record.Imports {
			canonicalImport := normalizeCanonical(imported)
			moduleID := "module:" + imported
			if len(packageFiles[canonicalImport]) > 0 {
				moduleID = "package:" + imported
				addNode(GraphNode{ID: moduleID, Kind: "package", Name: imported}, semanticScore(terms, imported))
			} else {
				addNode(GraphNode{ID: moduleID, Kind: "module", Name: imported}, semanticScore(terms, imported))
			}
			addEdge(fileID, moduleID, "imports")
		}

		for _, symbol := range record.Symbols {
			symbolID := symbolNodeID(symbol)
			symbolScore := semanticScore(terms, symbol.Name, symbol.QualifiedName, symbol.Kind, symbol.Container, symbol.Path)
			symbolScore += pathRankingAdjustment(path, terms)
			addNode(GraphNode{ID: symbolID, Kind: symbol.Kind, Path: path, Name: symbol.Name, Language: symbol.Language}, symbolScore)
			addEdge(fileID, symbolID, "defines")
		}

		for _, ref := range record.References {
			if ref.Kind == "identifier" {
				continue
			}
			from := fileID
			if ref.Container != "" {
				if caller := firstDefinition(definitionsByQualified[normalizeCanonical(ref.Container)]); caller != nil {
					from = symbolNodeID(*caller)
				}
			}
			targets := definitionsByQualified[normalizeCanonical(ref.QualifiedName)]
			if len(targets) == 0 && ref.Kind == "call" {
				// Method calls through variables/interfaces cannot be resolved from
				// syntax alone. A small, bounded name set is still useful for an
				// approximate repository flow without exploding the graph.
				candidates := definitionsByName[strings.ToLower(ref.Name)]
				if len(candidates) <= 8 {
					targets = candidates
				}
			}
			for _, target := range targets {
				targetID := symbolNodeID(target)
				addNode(GraphNode{ID: targetID, Kind: target.Kind, Path: target.Path, Name: target.Name, Language: target.Language}, semanticScore(terms, target.Name, target.QualifiedName, target.Path, target.Kind))
				kind := "references"
				if ref.Kind == "call" {
					kind = "calls"
				}
				addEdge(from, targetID, kind)
			}
		}

		lowerPath := strings.ToLower(path)
		if strings.Contains(lowerPath, "_test.") || strings.Contains(lowerPath, ".test.") || strings.Contains(lowerPath, "/tests/") || strings.HasPrefix(lowerPath, "tests/") {
			for _, candidate := range probableTestTargets(path) {
				if _, ok := idx.Files[candidate]; ok {
					addEdge(fileID, "file:"+candidate, "tests")
				}
			}
		}
	}

	adjacency := map[string][]string{}
	for _, edge := range edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		adjacency[edge.To] = append(adjacency[edge.To], edge.From)
	}
	type seed struct {
		id    string
		score int
	}
	seeds := make([]seed, 0, len(nodes))
	for id := range nodes {
		score := scores[id]
		if strings.TrimSpace(query) == "" || score > 0 {
			seeds = append(seeds, seed{id: id, score: score + centrality[id]})
		}
	}
	sort.SliceStable(seeds, func(i, j int) bool {
		if seeds[i].score != seeds[j].score {
			return seeds[i].score > seeds[j].score
		}
		return seeds[i].id < seeds[j].id
	})
	if len(seeds) == 0 {
		for id := range nodes {
			seeds = append(seeds, seed{id: id, score: centrality[id]})
		}
		sort.SliceStable(seeds, func(i, j int) bool { return seeds[i].score > seeds[j].score })
	}
	if len(seeds) > 24 {
		seeds = seeds[:24]
	}

	distance := map[string]int{}
	queue := make([]string, 0, len(seeds))
	for _, item := range seeds {
		if _, ok := distance[item.id]; ok {
			continue
		}
		distance[item.id] = 0
		queue = append(queue, item.id)
	}
	for len(queue) > 0 && len(distance) < limit*4 {
		current := queue[0]
		queue = queue[1:]
		if distance[current] >= 2 {
			continue
		}
		for _, neighbor := range adjacency[current] {
			if _, ok := distance[neighbor]; ok {
				continue
			}
			distance[neighbor] = distance[current] + 1
			queue = append(queue, neighbor)
		}
	}

	ranked := make([]GraphNode, 0, len(distance))
	for id, dist := range distance {
		node, ok := nodes[id]
		if !ok {
			continue
		}
		node.Weight = scores[id] + centrality[id] + (3-dist)*20
		ranked = append(ranked, node)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Weight != ranked[j].Weight {
			return ranked[i].Weight > ranked[j].Weight
		}
		return ranked[i].ID < ranked[j].ID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	allowed := map[string]bool{}
	for _, node := range ranked {
		allowed[node.ID] = true
	}
	filteredEdges := make([]GraphEdge, 0)
	for _, edge := range edges {
		if allowed[edge.From] && allowed[edge.To] {
			filteredEdges = append(filteredEdges, edge)
		}
	}
	return RepositoryGraph{Nodes: ranked, Edges: filteredEdges}, nil
}

func firstDefinition(values []Symbol) *Symbol {
	if len(values) == 0 {
		return nil
	}
	return &values[0]
}

func symbolNodeID(symbol Symbol) string {
	qualified := symbol.QualifiedName
	if qualified == "" {
		qualified = fmt.Sprintf("%s:%d:%s", symbol.Path, symbol.StartLine, symbol.Name)
	}
	return "symbol:" + qualified
}

func probableTestTargets(path string) []string {
	path = filepath.ToSlash(path)
	dir, base := filepath.ToSlash(filepath.Dir(path)), filepath.Base(path)
	var candidates []string
	for _, marker := range []string{"_test.", ".test.", ".spec."} {
		if idx := strings.Index(base, marker); idx >= 0 {
			candidate := base[:idx] + base[idx+len(marker)-1:]
			if dir == "." {
				candidates = append(candidates, candidate)
			} else {
				candidates = append(candidates, dir+"/"+candidate)
			}
		}
	}
	if strings.HasPrefix(path, "tests/") {
		candidates = append(candidates, strings.TrimPrefix(path, "tests/"))
	}
	return candidates
}
