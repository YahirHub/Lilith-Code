// Package knowledge provides a lazy, read-only knowledge base compiled into
// Lilith. It is intentionally independent from Agent Skills and project files.
package knowledge

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/lilith/li/assets"
)

const (
	defaultSearchLimit = 8
	maxSearchLimit     = 24
	defaultReadLines   = 160
	maxReadLines       = 500
	maxDocumentBytes   = 256 * 1024
	maxIndexedDocs     = 2000
	maxIndexBytes      = 32 * 1024 * 1024
	maxSnippetRunes    = 320
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type source struct {
	namespace string
	fs        fs.FS
}

var externalSources struct {
	sync.RWMutex
	items map[string]fs.FS
}

// RegisterNamespace adds a statically linked private namespace. A downstream
// module should call it from init after embedding its own read-only fs.FS.
// Registration is fail-closed: public and duplicate namespaces are rejected.
func RegisterNamespace(namespace string, sourceFS fs.FS) error {
	namespace = normalizeNamespace(namespace)
	if namespace == "" || !namespacePattern.MatchString(namespace) {
		return fmt.Errorf("invalid knowledge namespace %q", namespace)
	}
	if namespace == "public" {
		return errors.New("knowledge namespace public is reserved for built-in assets")
	}
	if info, err := fs.Stat(assets.KnowledgeFS(), namespace); err == nil && info.IsDir() {
		return fmt.Errorf("knowledge namespace %q already exists in embedded assets", namespace)
	}
	if sourceFS == nil {
		return fmt.Errorf("knowledge namespace %s has no filesystem", namespace)
	}
	externalSources.Lock()
	defer externalSources.Unlock()
	if externalSources.items == nil {
		externalSources.items = map[string]fs.FS{}
	}
	if _, exists := externalSources.items[namespace]; exists {
		return fmt.Errorf("duplicate knowledge namespace %q", namespace)
	}
	externalSources.items[namespace] = sourceFS
	return nil
}

// MustRegisterNamespace is convenient for build-tagged private packages: an
// invalid distribution fails during process initialization instead of silently
// hiding company documentation.
func MustRegisterNamespace(namespace string, sourceFS fs.FS) {
	if err := RegisterNamespace(namespace, sourceFS); err != nil {
		panic(err)
	}
}

type document struct {
	Namespace string
	Topic     string
	Path      string
	Title     string
	Text      string
	search    string
}

// Base owns one immutable snapshot of all namespaces available when it is
// created. The expensive walk and text normalization happen on the first
// Search or Topics call, never during Lilith startup.
type Base struct {
	root    fs.FS
	sources []source
	once    sync.Once
	docs    []document
	err     error
}

// NewBuiltin creates the process knowledge base from assets/knowledge plus
// namespaces registered by statically linked private modules.
func NewBuiltin() *Base {
	b := &Base{root: assets.KnowledgeFS()}
	externalSources.RLock()
	for namespace, sourceFS := range externalSources.items {
		b.sources = append(b.sources, source{namespace: namespace, fs: sourceFS})
	}
	externalSources.RUnlock()
	sort.Slice(b.sources, func(i, j int) bool { return b.sources[i].namespace < b.sources[j].namespace })
	return b
}

func (b *Base) Available() bool { return b != nil && b.root != nil }

// Search performs deterministic lexical ranking over the lazy index.
func (b *Base) Search(query, namespace, topic string, limit int) ([]Match, error) {
	if b == nil || b.root == nil {
		return nil, errors.New("knowledge base unavailable")
	}
	query = normalizeSearch(query)
	if query == "" {
		return nil, errors.New("knowledge query is empty")
	}
	namespace = normalizeNamespace(namespace)
	topic = normalizeSearch(topic)
	if namespace != "" && !namespacePattern.MatchString(namespace) {
		return nil, fmt.Errorf("invalid knowledge namespace %q", namespace)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	b.ensureIndex()
	if b.err != nil {
		return nil, b.err
	}
	terms := strings.Fields(query)
	type ranked struct {
		match Match
		score int
	}
	var rankedMatches []ranked
	for _, doc := range b.docs {
		if namespace != "" && doc.Namespace != namespace {
			continue
		}
		if topic != "" && normalizeSearch(doc.Topic) != topic {
			continue
		}
		score := scoreDocument(doc, query, terms)
		if score == 0 {
			continue
		}
		rankedMatches = append(rankedMatches, ranked{match: Match{
			Path: doc.Path, Namespace: doc.Namespace, Topic: doc.Topic,
			Title: doc.Title, Snippet: snippetFor(doc.Text, terms),
		}, score: score})
	}
	sort.SliceStable(rankedMatches, func(i, j int) bool {
		if rankedMatches[i].score != rankedMatches[j].score {
			return rankedMatches[i].score > rankedMatches[j].score
		}
		return rankedMatches[i].match.Path < rankedMatches[j].match.Path
	})
	if len(rankedMatches) > limit {
		rankedMatches = rankedMatches[:limit]
	}
	out := make([]Match, len(rankedMatches))
	for i := range rankedMatches {
		out[i] = rankedMatches[i].match
	}
	return out, nil
}

type Match struct {
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Topic     string `json:"topic"`
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
}

type ReadResult struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Lines      int    `json:"lines"`
	TotalLines int    `json:"total_lines"`
	Content    string `json:"content"`
	HasMore    bool   `json:"has_more"`
}

// Read loads a bounded line range directly. It does not build the search index.
func (b *Base) Read(canonicalPath string, offset, limit int) (ReadResult, error) {
	canonicalPath, err := cleanKnowledgePath(canonicalPath)
	if err != nil {
		return ReadResult{}, err
	}
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = defaultReadLines
	}
	if limit > maxReadLines {
		limit = maxReadLines
	}
	namespace, relative, _ := strings.Cut(canonicalPath, "/")
	data, err := b.readNamespaceFile(namespace, relative)
	if err != nil {
		return ReadResult{}, err
	}
	if len(data) > maxDocumentBytes {
		return ReadResult{}, fmt.Errorf("knowledge document exceeds %d bytes", maxDocumentBytes)
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return ReadResult{}, errors.New("knowledge document is not UTF-8 text")
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	total := len(lines)
	start := offset - 1
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return ReadResult{Path: canonicalPath, Offset: start + 1, Lines: end - start,
		TotalLines: total, Content: strings.Join(lines[start:end], "\n"), HasMore: end < total}, nil
}

type Topic struct {
	Namespace string `json:"namespace"`
	Topic     string `json:"topic"`
	Documents int    `json:"documents"`
}

func (b *Base) Topics(namespace string) ([]Topic, error) {
	if b == nil || b.root == nil {
		return nil, errors.New("knowledge base unavailable")
	}
	namespace = normalizeNamespace(namespace)
	if namespace != "" && !namespacePattern.MatchString(namespace) {
		return nil, fmt.Errorf("invalid knowledge namespace %q", namespace)
	}
	b.ensureIndex()
	if b.err != nil {
		return nil, b.err
	}
	counts := map[string]int{}
	for _, doc := range b.docs {
		if namespace != "" && doc.Namespace != namespace {
			continue
		}
		counts[doc.Namespace+"\x00"+doc.Topic]++
	}
	out := make([]Topic, 0, len(counts))
	for key, count := range counts {
		ns, topic, _ := strings.Cut(key, "\x00")
		out = append(out, Topic{Namespace: ns, Topic: topic, Documents: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Topic < out[j].Topic
	})
	return out, nil
}

func (b *Base) ensureIndex() { b.once.Do(b.buildIndex) }

func (b *Base) buildIndex() {
	entries, err := fs.ReadDir(b.root, ".")
	if err != nil {
		b.err = fmt.Errorf("list embedded knowledge namespaces: %w", err)
		return
	}
	var sources []source
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		namespace := normalizeNamespace(entry.Name())
		if !namespacePattern.MatchString(namespace) {
			continue
		}
		sub, subErr := fs.Sub(b.root, entry.Name())
		if subErr != nil {
			b.err = subErr
			return
		}
		sources = append(sources, source{namespace: namespace, fs: sub})
	}
	sources = append(sources, b.sources...)
	indexedBytes := 0
	for _, source := range sources {
		err = fs.WalkDir(source.fs, ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(path.Ext(name), ".md") {
				return nil
			}
			if len(b.docs) >= maxIndexedDocs {
				return fmt.Errorf("knowledge index exceeds %d documents", maxIndexedDocs)
			}
			data, readErr := fs.ReadFile(source.fs, name)
			if readErr != nil {
				return readErr
			}
			if len(data) > maxDocumentBytes || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
				return nil
			}
			indexedBytes += len(data)
			if indexedBytes > maxIndexBytes {
				return fmt.Errorf("knowledge index exceeds %d bytes", maxIndexBytes)
			}
			text := strings.ReplaceAll(string(data), "\r\n", "\n")
			topic := strings.Split(strings.TrimPrefix(name, "./"), "/")[0]
			if !strings.Contains(name, "/") {
				topic = "general"
			}
			title := firstMarkdownTitle(text)
			if title == "" {
				title = strings.TrimSuffix(path.Base(name), path.Ext(name))
			}
			canonical := source.namespace + "/" + strings.TrimPrefix(name, "./")
			b.docs = append(b.docs, document{Namespace: source.namespace, Topic: topic,
				Path: canonical, Title: title, Text: text,
				search: normalizeSearch(title + " " + canonical + " " + text)})
			return nil
		})
		if err != nil {
			b.err = fmt.Errorf("index knowledge namespace %s: %w", source.namespace, err)
			return
		}
	}
	sort.Slice(b.docs, func(i, j int) bool { return b.docs[i].Path < b.docs[j].Path })
}

func (b *Base) readNamespaceFile(namespace, relative string) ([]byte, error) {
	if b == nil || b.root == nil {
		return nil, errors.New("knowledge base unavailable")
	}
	if namespace == "public" {
		return fs.ReadFile(b.root, path.Join(namespace, relative))
	}
	for _, source := range b.sources {
		if source.namespace == namespace {
			return fs.ReadFile(source.fs, relative)
		}
	}
	// Allow additional namespaces placed directly under assets/knowledge in a
	// downstream tree, while preserving the explicit canonical namespace path.
	return fs.ReadFile(b.root, path.Join(namespace, relative))
}

func cleanKnowledgePath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	clean := path.Clean(name)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", errors.New("knowledge path must be namespace/relative-file.md")
	}
	namespace, relative, ok := strings.Cut(clean, "/")
	if !ok || relative == "" || !namespacePattern.MatchString(normalizeNamespace(namespace)) {
		return "", errors.New("knowledge path must include a valid namespace")
	}
	if !strings.EqualFold(path.Ext(relative), ".md") {
		return "", errors.New("knowledge_read accepts Markdown documents only")
	}
	return normalizeNamespace(namespace) + "/" + relative, nil
}

func scoreDocument(doc document, phrase string, terms []string) int {
	score := 0
	matchedTerms := 0
	title := normalizeSearch(doc.Title)
	canonical := normalizeSearch(doc.Path)
	if strings.Contains(title, phrase) {
		score += 120
	}
	if strings.Contains(canonical, phrase) {
		score += 80
	}
	if strings.Contains(doc.search, phrase) {
		score += 40
	}
	for _, term := range terms {
		if !strings.Contains(doc.search, term) {
			continue
		}
		matchedTerms++
		if strings.Contains(title, term) {
			score += 24
		} else if strings.Contains(canonical, term) {
			score += 14
		} else {
			score += 4
		}
	}
	if matchedTerms == 0 || (len(terms) > 1 && matchedTerms*2 < len(terms)) {
		return 0
	}
	return score
}

func snippetFor(text string, terms []string) string {
	lines := strings.Split(text, "\n")
	best := ""
	bestScore := -1
	for _, line := range lines {
		candidate := strings.TrimSpace(strings.TrimLeft(line, "#-* "))
		if candidate == "" {
			continue
		}
		normalized := normalizeSearch(candidate)
		score := 0
		for _, term := range terms {
			if strings.Contains(normalized, term) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	if best == "" {
		return ""
	}
	runes := []rune(best)
	if len(runes) > maxSnippetRunes {
		best = string(runes[:maxSnippetRunes-1]) + "…"
	}
	return best
}

func firstMarkdownTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func normalizeNamespace(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func normalizeSearch(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	space := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '+' || r == '#' || r == '-' {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}
