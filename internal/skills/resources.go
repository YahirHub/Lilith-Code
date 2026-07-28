package skills

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultSearchLimit   = 8
	maxSearchLimit       = 24
	maxContextLines      = 8
	defaultReadLines     = 120
	maxReadLines         = 500
	maxSearchFiles       = 5000
	maxSearchLineBytes   = 1024 * 1024
	maxSnippetLineBytes  = 2048
	maxSearchOutputBytes = 48 * 1024
	maxReadOutputBytes   = 64 * 1024
	binaryProbeBytes     = 8192
	maxCandidatesPerFile = 6
)

var skippedSkillDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	"__pycache__":  true,
}

var commonBinaryExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".bmp": true, ".pdf": true, ".zip": true, ".gz": true,
	".7z": true, ".rar": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".mp3": true, ".wav": true, ".mp4": true, ".webm": true,
	".mov": true, ".avi": true, ".exe": true, ".dll": true, ".so": true,
	".dylib": true,
}

// ResourceKind clasifica un archivo de skill por su ubicación/propósito.
type ResourceKind string

const (
	KindInstructions ResourceKind = "instructions"
	KindReference    ResourceKind = "reference"
	KindScript       ResourceKind = "script"
	KindAsset        ResourceKind = "asset"
	KindExample      ResourceKind = "example"
	KindTest         ResourceKind = "test"
	KindOther        ResourceKind = "other"
)

// FileInfo es metadata compacta de un recurso de una skill.
type FileInfo struct {
	Path string
	Kind ResourceKind
	Size int64
	Text bool
}

// ListFilesOptions controla el listado recursivo de una skill.
type ListFilesOptions struct {
	Path               string
	Pattern            string
	Kinds              []ResourceKind
	Extensions         []string
	Limit              int
	IncludeMaintenance bool
}

// SearchOptions controla la búsqueda determinista dentro de una skill.
type SearchOptions struct {
	Query              string
	Path               string
	Pattern            string
	Kinds              []ResourceKind
	Extensions         []string
	Regex              bool
	CaseSensitive      bool
	Limit              int
	ContextLines       int
	IncludeMaintenance bool
}

// SearchHit representa un resultado ordenado por relevancia.
type SearchHit struct {
	Path    string
	Kind    ResourceKind
	Line    int
	Score   int
	Snippet string
}

// SearchResult resume la búsqueda y mantiene el output acotado.
type SearchResult struct {
	Hits         []SearchHit
	ScannedFiles int
	TextFiles    int
	Truncated    bool
}

// ReadResult devuelve un rango de líneas sin cargar todo el recurso en contexto.
type ReadResult struct {
	Path          string
	Kind          ResourceKind
	Offset        int
	EndLine       int
	Total         int
	Content       string
	Binary        bool
	Size          int64
	Truncated     bool
	ByteTruncated bool
}

// ListFiles enumera recursos dentro de la raíz de una skill con filtros.
func ListFiles(skill Skill, opts ListFilesOptions) ([]FileInfo, bool, error) {
	root, err := skillScope(skill, opts.Path)
	if err != nil {
		return nil, false, err
	}
	limit := clamp(opts.Limit, 1, 2000, 200)
	kinds := kindSet(opts.Kinds)
	exts := extensionSet(opts.Extensions)
	pattern := strings.TrimSpace(filepath.ToSlash(opts.Pattern))
	var out []FileInfo
	truncated := false

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipSkillDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(out) >= limit {
			truncated = true
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(skill.BaseDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !opts.IncludeMaintenance && isMaintainerResource(rel) {
			return nil
		}
		if pattern != "" && !matchSkillPattern(pattern, rel) {
			return nil
		}
		kind := classifyResource(rel)
		if len(kinds) > 0 && !kinds[kind] {
			return nil
		}
		if len(exts) > 0 && !exts[normalizeExt(filepath.Ext(rel))] {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		text := isTextResource(path)
		out = append(out, FileInfo{Path: rel, Kind: kind, Size: info.Size(), Text: text})
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, false, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, truncated, nil
}

// Search recorre archivos de texto y nombres de assets, calcula un score local y
// devuelve únicamente los fragmentos más relevantes. No usa LLM ni embeddings.
func Search(skill Skill, opts SearchOptions) (SearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return SearchResult{}, errors.New("query is required")
	}
	root, err := skillScope(skill, opts.Path)
	if err != nil {
		return SearchResult{}, err
	}
	limit := clamp(opts.Limit, 1, maxSearchLimit, defaultSearchLimit)
	contextLines := opts.ContextLines
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > maxContextLines {
		contextLines = maxContextLines
	}
	kinds := kindSet(opts.Kinds)
	exts := extensionSet(opts.Extensions)
	pattern := strings.TrimSpace(filepath.ToSlash(opts.Pattern))
	matcher, err := newSearchMatcher(query, opts.Regex, opts.CaseSensitive)
	if err != nil {
		return SearchResult{}, err
	}

	result := SearchResult{}
	candidates := make([]SearchHit, 0, limit*4)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipSkillDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if result.ScannedFiles >= maxSearchFiles {
			result.Truncated = true
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(skill.BaseDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !opts.IncludeMaintenance && isMaintainerResource(rel) {
			return nil
		}
		if pattern != "" && !matchSkillPattern(pattern, rel) {
			return nil
		}
		kind := classifyResource(rel)
		if len(kinds) > 0 && !kinds[kind] {
			return nil
		}
		if len(exts) > 0 && !exts[normalizeExt(filepath.Ext(rel))] {
			return nil
		}
		result.ScannedFiles++

		pathScore := matcher.scorePath(rel)
		if !isTextResource(path) {
			if pathScore > 0 {
				candidates = append(candidates, SearchHit{Path: rel, Kind: kind, Score: pathScore})
			}
			return nil
		}
		result.TextFiles++
		fileHits, scanErr := scanTextFile(path, rel, kind, matcher, pathScore)
		if scanErr == nil {
			candidates = append(candidates, fileHits...)
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return SearchResult{}, err
	}

	if len(candidates) == 0 {
		return result, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Line < candidates[j].Line
	})

	// Evita devolver ventanas prácticamente idénticas del mismo archivo.
	selected := make([]SearchHit, 0, limit)
	for _, hit := range candidates {
		duplicate := false
		for _, prev := range selected {
			if hit.Path == prev.Path && hit.Line > 0 && prev.Line > 0 && abs(hit.Line-prev.Line) <= contextLines*2+1 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if hit.Line > 0 {
			full := filepath.Join(skill.BaseDir, filepath.FromSlash(hit.Path))
			hit.Snippet, _ = readContext(full, hit.Line, contextLines)
		}
		selected = append(selected, hit)
		if len(selected) >= limit {
			break
		}
	}
	result.Hits = selected
	return result, nil
}

// ReadFile lee un rango de líneas de un recurso de skill. Path vacío = SKILL.md.
func ReadFile(skill Skill, rel string, offset, limit int) (ReadResult, error) {
	if strings.TrimSpace(rel) == "" {
		rel = "SKILL.md"
	}
	full, err := resolveSkillPath(skill.BaseDir, rel)
	if err != nil {
		return ReadResult{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return ReadResult{}, err
	}
	if info.IsDir() {
		return ReadResult{}, fmt.Errorf("%s is a directory", rel)
	}
	normalizedRel, _ := filepath.Rel(skill.BaseDir, full)
	normalizedRel = filepath.ToSlash(normalizedRel)
	res := ReadResult{Path: normalizedRel, Kind: classifyResource(normalizedRel), Size: info.Size()}
	if !isTextResource(full) {
		res.Binary = true
		return res, nil
	}
	offset = clamp(offset, 1, int(^uint(0)>>1), 1)
	limit = clamp(limit, 1, maxReadLines, defaultReadLines)
	f, err := os.Open(full)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)
	var b strings.Builder
	lineNo := 0
	written := 0
	outputFull := false
	for scanner.Scan() {
		lineNo++
		if lineNo < offset || written >= limit || outputFull {
			continue
		}
		line, clipped := truncateBytes(scanner.Text(), maxSnippetLineBytes)
		needed := len(line)
		if written > 0 {
			needed++
		}
		if b.Len()+needed > maxReadOutputBytes {
			res.ByteTruncated = true
			outputFull = true
			continue
		}
		if written > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		written++
		if clipped {
			res.ByteTruncated = true
		}
	}
	if err := scanner.Err(); err != nil {
		return ReadResult{}, err
	}
	res.Offset = offset
	res.Total = lineNo
	res.EndLine = offset + written - 1
	if written == 0 {
		res.EndLine = 0
	}
	res.Content = b.String()
	res.Truncated = res.EndLine > 0 && res.EndLine < res.Total
	if res.ByteTruncated {
		res.Truncated = true
	}
	return res, nil
}

func scanTextFile(path, rel string, kind ResourceKind, matcher searchMatcher, pathScore int) ([]SearchHit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)
	var hits []SearchHit
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		score := matcher.scoreLine(line)
		if score <= 0 {
			continue
		}
		// El path aporta contexto adicional, pero no debe dominar el contenido.
		score += min(pathScore, 35)
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			score += 6
		}
		hits = append(hits, SearchHit{Path: rel, Kind: kind, Line: lineNo, Score: score})
		if len(hits) > maxCandidatesPerFile*3 {
			sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
			hits = hits[:maxCandidatesPerFile]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(hits) == 0 && pathScore > 0 {
		return []SearchHit{{Path: rel, Kind: kind, Score: pathScore}}, nil
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > maxCandidatesPerFile {
		hits = hits[:maxCandidatesPerFile]
	}
	return hits, nil
}

func readContext(path string, line, contextLines int) (string, error) {
	start := max(1, line-contextLines)
	end := line + contextLines
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)
	var b strings.Builder
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			break
		}
		marker := "  "
		if lineNo == line {
			marker = "> "
		}
		text, clipped := truncateBytes(scanner.Text(), maxSnippetLineBytes)
		if clipped {
			text += " …[line truncated]"
		}
		fmt.Fprintf(&b, "%s%d | %s\n", marker, lineNo, text)
	}
	return strings.TrimRight(b.String(), "\n"), scanner.Err()
}

type searchMatcher struct {
	query         string
	terms         []string
	regex         *regexp.Regexp
	caseSensitive bool
}

func newSearchMatcher(query string, regexMode, caseSensitive bool) (searchMatcher, error) {
	m := searchMatcher{caseSensitive: caseSensitive}
	if regexMode {
		pattern := query
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		rx, err := regexp.Compile(pattern)
		if err != nil {
			return searchMatcher{}, fmt.Errorf("invalid regex: %w", err)
		}
		m.regex = rx
		m.query = query
		return m, nil
	}
	m.query = normalizeSearch(query, caseSensitive)
	seen := map[string]bool{}
	for _, term := range strings.FieldsFunc(m.query, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.')
	}) {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		m.terms = append(m.terms, term)
	}
	if len(m.terms) == 0 {
		m.terms = []string{m.query}
	}
	return m, nil
}

func (m searchMatcher) scorePath(path string) int {
	if m.regex != nil {
		matches := m.regex.FindAllStringIndex(path, -1)
		if len(matches) == 0 {
			return 0
		}
		return 70 + min(len(matches), 5)*8
	}
	hay := normalizeSearch(filepath.ToSlash(path), m.caseSensitive)
	base := normalizeSearch(filepath.Base(path), m.caseSensitive)
	score := 0
	if strings.Contains(hay, m.query) {
		score += 80
	}
	if strings.Contains(base, m.query) {
		score += 45
	}
	for _, term := range m.terms {
		if strings.Contains(hay, term) {
			score += 12
		}
		if strings.Contains(base, term) {
			score += 8
		}
	}
	return score
}

func (m searchMatcher) scoreLine(line string) int {
	if m.regex != nil {
		matches := m.regex.FindAllStringIndex(line, -1)
		if len(matches) == 0 {
			return 0
		}
		return 90 + min(len(matches), 6)*10
	}
	hay := normalizeSearch(line, m.caseSensitive)
	score := 0
	if strings.Contains(hay, m.query) {
		score += 100
	}
	matched := 0
	for _, term := range m.terms {
		count := strings.Count(hay, term)
		if count == 0 {
			continue
		}
		matched++
		score += 14 + min(count, 4)*4
	}
	if matched == len(m.terms) && matched > 1 {
		score += 35
	}
	return score
}

func normalizeSearch(s string, caseSensitive bool) string {
	s = strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ü", "U", "Ñ", "N",
		"“", `"`, "”", `"`, "‘", "'", "’", "'", "–", "-", "—", "-",
	).Replace(s)
	if !caseSensitive {
		s = strings.ToLower(s)
	}
	return s
}

func skillScope(skill Skill, rel string) (string, error) {
	if strings.TrimSpace(skill.BaseDir) == "" {
		return "", errors.New("skill has no base directory")
	}
	if strings.TrimSpace(rel) == "" || rel == "." {
		return filepath.Clean(skill.BaseDir), nil
	}
	return resolveSkillPath(skill.BaseDir, rel)
}

func resolveSkillPath(root, rel string) (string, error) {
	root = filepath.Clean(root)
	candidate := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	inside, err := filepath.Rel(root, candidate)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the selected skill")
	}

	// Resolve symlinks for existing resources. A skill is allowed to link to
	// another resource inside itself, but never to turn skill_read into an
	// arbitrary filesystem reader outside the selected skill root.
	realRoot, rootErr := filepath.EvalSymlinks(root)
	realCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if rootErr == nil && candidateErr == nil {
		realInside, relErr := filepath.Rel(realRoot, realCandidate)
		if relErr != nil || realInside == ".." || strings.HasPrefix(realInside, ".."+string(filepath.Separator)) {
			return "", errors.New("path escapes the selected skill through a symlink")
		}
		candidate = realCandidate
	}
	return candidate, nil
}

func isMaintainerResource(rel string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(rel)))
	switch base {
	case "agents.md", "claude.md", "readme.md", "tutorial_skill.md", "tutorial-skill.md":
		return true
	default:
		return false
	}
}

func classifyResource(rel string) ResourceKind {
	rel = filepath.ToSlash(rel)
	if strings.EqualFold(rel, "SKILL.md") {
		return KindInstructions
	}
	first := strings.ToLower(strings.Split(rel, "/")[0])
	switch first {
	case "references", "reference", "docs", "documentation":
		return KindReference
	case "scripts", "script", "bin":
		return KindScript
	case "assets", "asset", "templates", "snippets":
		return KindAsset
	case "examples", "example":
		return KindExample
	case "tests", "test":
		return KindTest
	default:
		return KindOther
	}
}

func isTextResource(path string) bool {
	if commonBinaryExt[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, binaryProbeBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return false
		}
	}
	return true
}

func shouldSkipSkillDir(name string) bool {
	if skippedSkillDirs[name] {
		return true
	}
	return strings.HasPrefix(name, ".")
}

func kindSet(kinds []ResourceKind) map[ResourceKind]bool {
	if len(kinds) == 0 {
		return nil
	}
	out := map[ResourceKind]bool{}
	for _, kind := range kinds {
		if kind != "" {
			out[kind] = true
		}
	}
	return out
}

func extensionSet(exts []string) map[string]bool {
	if len(exts) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, ext := range exts {
		ext = normalizeExt(ext)
		if ext != "" {
			out[ext] = true
		}
	}
	return out
}

func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func matchSkillPattern(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	rel = filepath.ToSlash(rel)
	if pattern == "" {
		return true
	}
	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(rel))
		return ok
	}
	rx, err := regexp.Compile(skillGlobRegex(pattern))
	return err == nil && rx.MatchString(rel)
}

func skillGlobRegex(pattern string) string {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					b.WriteString("(?:.*/)?")
					i++
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			start := i
			for i < len(pattern) && pattern[i] != '*' && pattern[i] != '?' {
				i++
			}
			b.WriteString(regexp.QuoteMeta(pattern[start:i]))
		}
	}
	b.WriteByte('$')
	return b.String()
}

func truncateBytes(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	if cut <= 0 {
		return "", true
	}
	return s[:cut], true
}

func clamp(v, minV, maxV, def int) int {
	if v == 0 {
		return def
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// FormatSearchResult produce un resultado estable y compacto para tool output.
func FormatSearchResult(skill Skill, query string, result SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "skill: %s\nquery: %s\nscanned_files: %d\ntext_files: %d\n", skill.Name, strconv.Quote(query), result.ScannedFiles, result.TextFiles)
	if result.Truncated {
		b.WriteString("note: scan limit reached; narrow path/pattern if needed\n")
	}
	if len(result.Hits) == 0 {
		b.WriteString("matches: 0\n")
		return b.String()
	}
	fmt.Fprintf(&b, "matches: %d\n\n", len(result.Hits))
	for i, hit := range result.Hits {
		location := hit.Path
		if hit.Line > 0 {
			location += ":" + strconv.Itoa(hit.Line)
		}
		var entry strings.Builder
		fmt.Fprintf(&entry, "%d. %s [%s] score=%d\n", i+1, location, hit.Kind, hit.Score)
		if hit.Snippet != "" {
			entry.WriteString(hit.Snippet)
			entry.WriteByte('\n')
		}
		if i < len(result.Hits)-1 {
			entry.WriteByte('\n')
		}
		if b.Len()+entry.Len() > maxSearchOutputBytes {
			b.WriteString("[output truncated; narrow path/pattern/kinds/extensions or lower context_lines]\n")
			break
		}
		b.WriteString(entry.String())
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
