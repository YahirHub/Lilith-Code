package codeintel

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Symbols finds declarations by fuzzy name, qualified name, kind and/or path.
func (m *Manager) Symbols(ctx context.Context, query, pathFilter, kind string, limit int) ([]Symbol, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if _, err := m.EnsureFresh(ctx); err != nil && ctx.Err() != nil {
		return nil, err
	}
	idx := m.Snapshot()
	query = strings.TrimSpace(query)
	pathFilter = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathFilter)))
	kind = strings.ToLower(strings.TrimSpace(kind))
	terms := semanticTerms(query)
	type scored struct {
		s Symbol
		n int
	}
	var found []scored
	for _, path := range sortedFileKeys(idx.Files) {
		record := idx.Files[path]
		if pathFilter != "" && !strings.Contains(strings.ToLower(path), pathFilter) {
			continue
		}
		for _, symbol := range record.Symbols {
			if kind != "" && !strings.EqualFold(symbol.Kind, kind) {
				continue
			}
			score := matchScore(strings.ToLower(query), symbol.Name, symbol.QualifiedName, symbol.Path, symbol.Kind)
			score += semanticScore(terms, symbol.Name, symbol.QualifiedName, symbol.Container, symbol.Path, symbol.Kind)
			if query != "" && score <= 0 {
				continue
			}
			found = append(found, scored{symbol, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].n != found[j].n {
			return found[i].n > found[j].n
		}
		if found[i].s.Path != found[j].s.Path {
			return found[i].s.Path < found[j].s.Path
		}
		return found[i].s.StartLine < found[j].s.StartLine
	})
	if len(found) > limit {
		found = found[:limit]
	}
	out := make([]Symbol, len(found))
	for i := range found {
		out[i] = found[i].s
	}
	return out, nil
}

// References resolves definitions first and then returns references tied to the
// same qualified symbol. Bare-name fallback is used only when syntax cannot
// provide a qualified target.
func (m *Manager) References(ctx context.Context, name string, limit int) ([]Reference, []Symbol, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("symbol name is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if _, err := m.EnsureFresh(ctx); err != nil && ctx.Err() != nil {
		return nil, nil, err
	}
	idx := m.Snapshot()
	qualifiedQuery := strings.ContainsAny(name, ".//")
	var defs []Symbol
	for _, path := range sortedFileKeys(idx.Files) {
		for _, symbol := range idx.Files[path].Symbols {
			if definitionMatchesQuery(name, symbol, qualifiedQuery) {
				defs = append(defs, symbol)
			}
		}
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Path != defs[j].Path {
			return defs[i].Path < defs[j].Path
		}
		return defs[i].StartLine < defs[j].StartLine
	})

	targets := map[string]bool{}
	packages := map[string]bool{}
	for _, def := range defs {
		if def.QualifiedName != "" {
			targets[normalizeCanonical(def.QualifiedName)] = true
		}
		if def.Package != "" {
			packages[strings.ToLower(def.Package)] = true
		}
	}
	var refs []Reference
	for _, path := range sortedFileKeys(idx.Files) {
		for _, ref := range idx.Files[path].References {
			if referenceMatchesQuery(name, ref, qualifiedQuery, targets, packages) {
				refs = append(refs, ref)
				if len(refs)+len(defs) >= limit {
					return refs, trimSymbols(defs, limit-len(refs)), nil
				}
			}
		}
	}
	return refs, trimSymbols(defs, limit-len(refs)), nil
}

func definitionMatchesQuery(query string, symbol Symbol, qualified bool) bool {
	if qualified {
		if canonicalNameMatches(query, symbol.QualifiedName) {
			return true
		}
		return canonicalNameMatches(query, strings.TrimSpace(symbol.Package)+"."+symbol.Name)
	}
	return identifierEqual(query, symbol.Name)
}

func referenceMatchesQuery(query string, ref Reference, qualified bool, targets, packages map[string]bool) bool {
	if qualified {
		if canonicalNameMatches(query, ref.QualifiedName) {
			return true
		}
		if ref.Receiver != "" && canonicalNameMatches(query, ref.Receiver+"."+ref.Name) {
			return true
		}
		return false
	}
	if !identifierEqual(query, ref.Name) {
		return false
	}
	if len(targets) == 0 {
		return true
	}
	if ref.QualifiedName != "" && targets[normalizeCanonical(ref.QualifiedName)] {
		return true
	}
	return ref.QualifiedName == "" && (len(packages) == 0 || packages[strings.ToLower(ref.Package)])
}

func trimSymbols(values []Symbol, limit int) []Symbol {
	if limit < 0 {
		return nil
	}
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func identifierEqual(query, value string) bool {
	if hasUpper(query) {
		return strings.TrimSpace(query) == strings.TrimSpace(value)
	}
	return strings.EqualFold(strings.TrimSpace(query), strings.TrimSpace(value))
}

func hasUpper(value string) bool {
	for _, r := range value {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func canonicalNameMatches(query, candidate string) bool {
	originalQuery := strings.TrimSpace(query)
	originalCandidate := strings.TrimSpace(candidate)
	if originalQuery == "" || originalCandidate == "" {
		return false
	}
	if hasUpper(originalQuery) && canonicalLast(originalQuery) != canonicalLast(originalCandidate) {
		return false
	}
	query = normalizeCanonical(originalQuery)
	candidate = normalizeCanonical(originalCandidate)
	if candidate == query {
		return true
	}
	if strings.HasSuffix(candidate, "/"+query) || strings.HasSuffix(candidate, "."+query) {
		return true
	}
	return false
}

func canonicalLast(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.LastIndexAny(value, "./"); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

func normalizeCanonical(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.Trim(value, "./")
	return strings.ToLower(value)
}

func matchScore(query string, values ...string) int {
	if query == "" {
		return 1
	}
	score := 0
	for idx, value := range values {
		v := strings.ToLower(value)
		switch {
		case v == query:
			score += 100 - idx*5
		case strings.HasSuffix(v, "/"+query) || strings.HasSuffix(v, "."+query):
			score += 90 - idx*5
		case strings.HasPrefix(v, query):
			score += 70 - idx*5
		case strings.Contains(v, query):
			score += 40 - idx*5
		}
	}
	return score
}

// Context selects ranked, syntax-bounded snippets. It scores code identifiers,
// qualified references and paths by task terms, while strongly deprioritizing
// documentation for implementation queries.
func (m *Manager) Context(ctx context.Context, query string, paths []string, maxChars int) ([]ContextChunk, error) {
	if maxChars <= 0 || maxChars > 120000 {
		maxChars = 24000
	}
	if _, err := m.EnsureFresh(ctx); err != nil && ctx.Err() != nil {
		return nil, err
	}
	idx := m.Snapshot()
	changed := gitChangedFiles(ctx, m.root)
	pathFilters := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.ToLower(filepath.ToSlash(strings.TrimSpace(p)))
		if p != "" {
			pathFilters = append(pathFilters, p)
		}
	}
	terms := semanticTerms(query)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	refCounts := map[string]int{}
	for _, record := range idx.Files {
		for _, ref := range record.References {
			key := normalizeCanonical(firstNonEmpty(ref.QualifiedName, ref.Name))
			refCounts[key]++
		}
	}
	type candidate struct {
		symbol Symbol
		score  int
	}
	var candidates []candidate
	fileScores := map[string]int{}
	for _, path := range sortedFileKeys(idx.Files) {
		record := idx.Files[path]
		if len(pathFilters) > 0 && !matchesAnyPath(path, pathFilters) {
			continue
		}
		if len(pathFilters) == 0 && !taskAllowsPath(path, terms) {
			continue
		}
		fileScore := semanticScore(terms, recordSemanticValues(record)...)
		fileScore += pathRankingAdjustment(path, terms)
		if queryLower == "" {
			fileScore = maxInt(fileScore, 5)
		}
		fileScores[path] = fileScore
		for _, symbol := range record.Symbols {
			direct := matchScore(queryLower, symbol.Name, symbol.QualifiedName, symbol.Path, symbol.Kind)
			direct += semanticScore(terms, symbol.Name, symbol.QualifiedName, symbol.Container, symbol.Kind, symbol.Path)
			score := direct*2 + maxInt(0, fileScore/3)
			if isTestPath(path) && terms["test"] == 0 {
				score /= 3
			} else if !isTestPath(path) {
				score += 20
			}
			if queryLower == "" {
				score = 5
			}
			if changed[path] && score > 0 {
				score += 12
			}
			score += minInt(refCounts[normalizeCanonical(firstNonEmpty(symbol.QualifiedName, symbol.Name))], 24)
			if score > 0 {
				candidates = append(candidates, candidate{symbol, score})
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].symbol.Path != candidates[j].symbol.Path {
			return candidates[i].symbol.Path < candidates[j].symbol.Path
		}
		return candidates[i].symbol.StartLine < candidates[j].symbol.StartLine
	})

	var chunks []ContextChunk
	used := 0
	seen := map[string]bool{}
	perPath := map[string]int{}
	for _, item := range candidates {
		if used >= maxChars {
			break
		}
		pathLimit := 3
		if isTestPath(item.symbol.Path) && terms["test"] == 0 {
			pathLimit = 1
		}
		if perPath[item.symbol.Path] >= pathLimit {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", item.symbol.Path, item.symbol.StartLine, item.symbol.EndLine)
		if seen[key] {
			continue
		}
		seen[key] = true
		content, start, end, err := readProjectSnippet(m.root, item.symbol.Path, item.symbol.StartLine, item.symbol.EndLine, maxChars-used)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		chunks = append(chunks, ContextChunk{Path: item.symbol.Path, Language: item.symbol.Language, Symbol: item.symbol.Name, Kind: item.symbol.Kind, StartLine: start, EndLine: end, Score: item.score, Content: content})
		perPath[item.symbol.Path]++
		used += len(content)
		if len(chunks) >= 24 {
			break
		}
	}

	if len(chunks) == 0 {
		type fileCandidate struct {
			path  string
			score int
		}
		var files []fileCandidate
		for _, path := range sortedFileKeys(idx.Files) {
			if len(pathFilters) > 0 && !matchesAnyPath(path, pathFilters) {
				continue
			}
			if len(pathFilters) == 0 && !taskAllowsPath(path, terms) {
				continue
			}
			score := fileScores[path]
			if score <= 0 && !changed[path] {
				continue
			}
			files = append(files, fileCandidate{path, score})
		}
		sort.SliceStable(files, func(i, j int) bool {
			if files[i].score != files[j].score {
				return files[i].score > files[j].score
			}
			return files[i].path < files[j].path
		})
		for _, item := range files {
			record := idx.Files[item.path]
			content, start, end, err := readProjectSnippet(m.root, item.path, 1, minInt(record.LineCount, 160), maxChars-used)
			if err != nil || content == "" {
				continue
			}
			chunks = append(chunks, ContextChunk{Path: item.path, Language: record.Language, StartLine: start, EndLine: end, Score: item.score, Content: content})
			used += len(content)
			if used >= maxChars || len(chunks) >= 12 {
				break
			}
		}
	}
	return chunks, nil
}

func matchesAnyPath(path string, filters []string) bool {
	value := strings.ToLower(filepath.ToSlash(path))
	for _, filter := range filters {
		if strings.Contains(value, filter) {
			return true
		}
	}
	return false
}

func readProjectSnippet(root, relative string, startLine, endLine, maxChars int) (string, int, int, error) {
	if !safeIndexedPath(relative) {
		return "", 0, 0, fmt.Errorf("unsafe indexed path %q", relative)
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", 0, 0, err
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		resolvedRoot = filepath.Clean(root)
	}
	rel, relErr := filepath.Rel(resolvedRoot, resolved)
	if pathEscapesRoot(rel, relErr) {
		return "", 0, 0, fmt.Errorf("indexed path resolves outside the project root")
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", 0, 0, statErr
	}
	if !info.Mode().IsRegular() || info.Size() > maxIndexedFileBytes {
		return "", 0, 0, fmt.Errorf("indexed path is not a supported regular file")
	}
	return readSnippet(resolved, startLine, endLine, maxChars)
}

func readSnippet(path string, startLine, endLine, maxChars int) (string, int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, 0, err
	}
	lines := SourceLines(data)
	if len(lines) == 0 {
		return "", 0, 0, nil
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	startLine = maxInt(1, startLine-3)
	endLine = minInt(len(lines), endLine+5)
	if endLine-startLine > 240 {
		endLine = startLine + 240
	}
	var b strings.Builder
	actualEnd := startLine - 1
	for i := startLine; i <= endLine; i++ {
		line := fmt.Sprintf("%d: %s\n", i, lines[i-1])
		if maxChars > 0 && b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
		actualEnd = i
	}
	return b.String(), startLine, actualEnd, nil
}

func gitChangedFiles(ctx context.Context, root string) map[string]bool {
	result := map[string]bool{}
	git, err := exec.LookPath("git")
	if err != nil {
		return result
	}
	cmd := exec.CommandContext(ctx, git, "status", "--porcelain=v1", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	return parseGitPorcelainZ(out)
}

func parseGitPorcelainZ(out []byte) map[string]bool {
	result := map[string]bool{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		item := fields[i]
		if len(item) < 4 {
			continue
		}
		status := item[:2]
		path := string(item[3:])
		if path != "" {
			result[filepath.ToSlash(path)] = true
		}
		if (status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C') && i+1 < len(fields) {
			i++
			original := string(fields[i])
			if original != "" {
				result[filepath.ToSlash(original)] = true
			}
		}
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
