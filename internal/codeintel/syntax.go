package codeintel

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var importPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*import\s+(?:[\w*{},\s]+\s+from\s+)?["']([^"']+)["']`),
	regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+)?import\s+([\w.*]+)`),
	regexp.MustCompile(`(?m)^\s*use\s+([^;]+);`),
	regexp.MustCompile(`(?m)^\s*#include\s*[<"]([^>"]+)[>"]`),
	regexp.MustCompile(`(?m)^\s*(?:require|require_once|include|include_once)\s*\(?["']([^"']+)["']`),
	regexp.MustCompile(`(?m)^\s*using\s+([\w.]+)\s*;`),
}

func parseFile(root, full, rel, detectedLanguage string, info os.FileInfo) (record FileRecord, err error) {
	source, err := os.ReadFile(full)
	if err != nil {
		return FileRecord{Path: rel, Language: detectedLanguage, Size: info.Size(), ModUnixNano: info.ModTime().UnixNano()}, err
	}
	hash := sha256.Sum256(source)
	record = FileRecord{
		Path: rel, Language: detectedLanguage, Size: info.Size(), ModUnixNano: info.ModTime().UnixNano(),
		SHA256: hex.EncodeToString(hash[:]), LineCount: lineCount(source), Imports: extractImports(string(source)), Backend: "fallback",
	}
	defer func() {
		if detectedLanguage == "go" {
			precise, goErr := parseGoSyntax(root, full, rel, source)
			if precise.Package != "" || len(precise.Symbols) > 0 || len(precise.References) > 0 {
				record.Package = precise.Package
				record.PackagePath = precise.PackagePath
				record.ImportAliases = precise.ImportAliases
				record.Imports = precise.Imports
				record.Symbols = precise.Symbols
				record.References = precise.References
				if strings.Contains(record.Backend, "tree-sitter") {
					record.Backend = "tree-sitter+go-ast"
				} else {
					record.Backend = "go-ast"
				}
			}
			if err == nil && goErr != nil {
				err = goErr
			}
		}
		dedupeSyntax(&record)
	}()

	entry := grammars.DetectLanguage(full)
	if entry == nil && len(source) > 0 {
		first := string(source)
		if idx := strings.IndexByte(first, '\n'); idx >= 0 {
			first = first[:idx]
		}
		entry = grammars.DetectLanguageByShebang(first)
	}
	if entry == nil || entry.Language == nil {
		record.Symbols, record.References = fallbackExtract(rel, detectedLanguage, source)
		return record, nil
	}
	lang, tree, parseErr := parseEmbeddedGrammar(entry, source)
	if lang == nil {
		// The grammar is known by extension but not part of the selected embedded
		// grammar set. Falling back is normal and should not look like a broken file.
		record.Grammar = entry.Name
		record.Symbols, record.References = fallbackExtract(rel, detectedLanguage, source)
		return record, parseErr
	}
	record.Backend = "tree-sitter"
	record.Grammar = entry.Name
	err = parseErr
	if err != nil {
		record.Backend = "fallback"
		record.Symbols, record.References = fallbackExtract(rel, detectedLanguage, source)
		return record, err
	}
	if tree == nil {
		record.Backend = "fallback"
		record.Symbols, record.References = fallbackExtract(rel, detectedLanguage, source)
		return record, nil
	}
	defer tree.Release()
	treeRoot := tree.RootNode()
	if treeRoot != nil {
		record.HasErrors = treeRoot.HasError()
	}
	for _, span := range gotreesitter.ExtractDefinitionSpans(tree) {
		name := cleanSymbolName(span.Name)
		if name == "" {
			continue
		}
		record.Symbols = append(record.Symbols, Symbol{
			Name: name, Kind: normalizeKind(span.Kind, span.NodeType), Language: firstNonEmpty(span.Lang, entry.Name, detectedLanguage), Path: rel,
			StartLine: byteLine(source, span.StartByte), EndLine: byteLine(source, safeEnd(span.StartByte, span.EndByte)),
			StartByte: span.StartByte, EndByte: span.EndByte, NodeType: span.NodeType,
		})
	}
	for _, call := range gotreesitter.ExtractCalls(tree) {
		name := cleanSymbolName(call.Name)
		if name == "" {
			continue
		}
		record.References = append(record.References, Reference{
			Name: name, Receiver: cleanSymbolName(call.Receiver), Kind: firstNonEmpty(call.Kind, "call"),
			Language: firstNonEmpty(call.Lang, entry.Name, detectedLanguage), Path: rel,
			StartLine: byteLine(source, call.StartByte), EndLine: byteLine(source, safeEnd(call.StartByte, call.EndByte)),
			StartByte: call.StartByte, EndByte: call.EndByte,
		})
	}
	appendTags(&record, tree, lang, *entry, source, rel, detectedLanguage)
	if len(record.Symbols) == 0 && len(record.References) == 0 {
		record.Symbols, record.References = fallbackExtract(rel, detectedLanguage, source)
		if len(record.Symbols) > 0 || len(record.References) > 0 {
			record.Backend = "tree-sitter+fallback"
		}
	}
	dedupeSyntax(&record)
	return record, nil
}

func parseEmbeddedGrammar(entry *grammars.LangEntry, source []byte) (lang *gotreesitter.Language, tree *gotreesitter.Tree, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			lang = nil
			tree = nil
			err = fmt.Errorf("embedded grammar %s failed: %v", entry.Name, recovered)
		}
	}()
	lang = entry.Language()
	if lang == nil {
		return nil, nil, nil
	}
	parser := gotreesitter.NewParser(lang)
	parser.SetTimeoutMicros(2_000_000)
	if entry.TokenSourceFactory != nil {
		tree, err = parser.ParseWithTokenSourceFactory(source, adaptTokenSourceFactory(entry, lang))
	} else {
		tree, err = parser.Parse(source)
	}
	return lang, tree, err
}

func adaptTokenSourceFactory(entry *grammars.LangEntry, lang *gotreesitter.Language) gotreesitter.TokenSourceFactory {
	return func(source []byte) (gotreesitter.TokenSource, error) {
		tokenSource := entry.TokenSourceFactory(source, lang)
		if tokenSource == nil {
			return nil, fmt.Errorf("embedded grammar %s returned a nil token source", entry.Name)
		}
		return tokenSource, nil
	}
}

func appendTags(record *FileRecord, tree *gotreesitter.Tree, lang *gotreesitter.Language, entry grammars.LangEntry, source []byte, rel, detectedLanguage string) {
	defer func() { _ = recover() }()
	query := strings.TrimSpace(grammars.ResolveTagsQuery(entry))
	if query == "" {
		return
	}
	options := []gotreesitter.TaggerOption{gotreesitter.WithTaggerTimeoutMicros(2_000_000)}
	if entry.TokenSourceFactory != nil {
		options = append(options, gotreesitter.WithTaggerTokenSourceFactory(func(src []byte) gotreesitter.TokenSource {
			return entry.TokenSourceFactory(src, lang)
		}))
	}
	tagger, err := gotreesitter.NewTagger(lang, query, options...)
	if err != nil {
		return
	}
	for _, tag := range tagger.TagTree(tree) {
		name := cleanSymbolName(tag.Name)
		if name == "" {
			continue
		}
		start, end := tag.Range.StartByte, tag.Range.EndByte
		if end < start || int(start) > len(source) || int(end) > len(source) {
			continue
		}
		language := firstNonEmpty(entry.Name, detectedLanguage)
		switch {
		case strings.HasPrefix(tag.Kind, "definition."):
			kind := normalizeKind(strings.TrimPrefix(tag.Kind, "definition."), "")
			record.Symbols = append(record.Symbols, Symbol{
				Name: name, Kind: kind, Language: language, Path: rel,
				StartLine: byteLine(source, start), EndLine: byteLine(source, safeEnd(start, end)),
				StartByte: start, EndByte: end, NodeType: tag.Kind,
			})
		case strings.HasPrefix(tag.Kind, "reference."):
			kind := strings.TrimPrefix(tag.Kind, "reference.")
			record.References = append(record.References, Reference{
				Name: name, Kind: firstNonEmpty(kind, "reference"), Language: language, Path: rel,
				StartLine: byteLine(source, start), EndLine: byteLine(source, safeEnd(start, end)),
				StartByte: start, EndByte: end,
			})
		}
	}
}

func safeEnd(start, end uint32) uint32 {
	if end > start {
		return end - 1
	}
	return start
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeKind(kind, nodeType string) string {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(kind, nodeType)))
	switch {
	case strings.Contains(value, "method"):
		return "method"
	case strings.Contains(value, "func") || strings.Contains(value, "function"):
		return "function"
	case strings.Contains(value, "class"):
		return "class"
	case strings.Contains(value, "interface"):
		return "interface"
	case strings.Contains(value, "struct"):
		return "struct"
	case strings.Contains(value, "enum"):
		return "enum"
	case strings.Contains(value, "type"):
		return "type"
	case strings.Contains(value, "module") || strings.Contains(value, "namespace"):
		return "module"
	case strings.Contains(value, "const"):
		return "constant"
	case strings.Contains(value, "variable") || strings.Contains(value, "field"):
		return "variable"
	default:
		return value
	}
}

func cleanSymbolName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "`\"'(){}[];,")
	if len(name) > 240 || strings.ContainsAny(name, "\n\r\t") {
		return ""
	}
	return name
}

func lineCount(source []byte) int {
	if len(source) == 0 {
		return 0
	}
	return 1 + strings.Count(string(source), "\n")
}

func byteLine(source []byte, offset uint32) int {
	if int(offset) > len(source) {
		offset = uint32(len(source))
	}
	return 1 + strings.Count(string(source[:offset]), "\n")
}

func extractImports(source string) []string {
	set := map[string]bool{}
	for _, pattern := range importPatterns {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			for _, value := range match[1:] {
				value = strings.TrimSpace(value)
				if value != "" {
					set[value] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

var fallbackDefinitionPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"function", regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:func|function|fn|def)\s+([A-Za-z_][\w]*)`)},
	{"class", regexp.MustCompile(`(?m)^\s*(?:export\s+)?class\s+([A-Za-z_][\w]*)`)},
	{"interface", regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+([A-Za-z_][\w]*)`)},
	{"struct", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?struct\s+([A-Za-z_][\w]*)`)},
	{"enum", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?enum\s+([A-Za-z_][\w]*)`)},
	{"type", regexp.MustCompile(`(?m)^\s*(?:export\s+)?type\s+([A-Za-z_][\w]*)`)},
	{"function", regexp.MustCompile(`(?m)^\s*(?:public|private|protected|static|final|abstract|virtual|override|async|\s)+\s*[\w<>,.?\[\]]+\s+([A-Za-z_][\w]*)\s*\(`)},
}

var fallbackCallPattern = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

func fallbackExtract(rel, language string, source []byte) ([]Symbol, []Reference) {
	var symbols []Symbol
	for _, item := range fallbackDefinitionPatterns {
		for _, loc := range item.re.FindAllSubmatchIndex(source, -1) {
			if len(loc) < 4 || loc[2] < 0 {
				continue
			}
			name := string(source[loc[2]:loc[3]])
			symbols = append(symbols, Symbol{Name: name, Kind: item.kind, Language: language, Path: rel, StartLine: byteLine(source, uint32(loc[0])), EndLine: byteLine(source, uint32(loc[1]-1)), StartByte: uint32(loc[0]), EndByte: uint32(loc[1])})
		}
	}
	keywords := map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true, "sizeof": true, "typeof": true, "func": true, "function": true}
	var refs []Reference
	for _, loc := range fallbackCallPattern.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		name := string(source[loc[2]:loc[3]])
		if keywords[name] {
			continue
		}
		refs = append(refs, Reference{Name: name, Kind: "call", Language: language, Path: rel, StartLine: byteLine(source, uint32(loc[0])), EndLine: byteLine(source, uint32(loc[1]-1)), StartByte: uint32(loc[0]), EndByte: uint32(loc[1])})
	}
	return symbols, refs
}

func dedupeSyntax(record *FileRecord) {
	seenSymbols := map[string]bool{}
	outSymbols := record.Symbols[:0]
	for _, item := range record.Symbols {
		key := fmt.Sprintf("%s\x00%s\x00%d", strings.ToLower(item.Name), item.Kind, item.StartByte)
		if seenSymbols[key] {
			continue
		}
		seenSymbols[key] = true
		outSymbols = append(outSymbols, item)
	}
	record.Symbols = outSymbols
	seenRefs := map[string]bool{}
	outRefs := record.References[:0]
	for _, item := range record.References {
		key := fmt.Sprintf("%s\x00%s\x00%d", strings.ToLower(item.Name), item.Receiver, item.StartByte)
		if seenRefs[key] {
			continue
		}
		seenRefs[key] = true
		outRefs = append(outRefs, item)
	}
	record.References = outRefs
}

// SourceLines returns a line-indexed slice without scanner token limits.
func SourceLines(source []byte) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(source)))
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxIndexedFileBytes+1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}
