package codeintel

import (
	"context"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// builtinGoSemantic provides a static, dependency-free fallback when gopls is
// not installed. It reuses Lilith's indexed canonical identities for symbols,
// definitions and references, and the standard Go parser for syntax
// diagnostics. It does not claim compiler-complete type information.
func (m *Manager) builtinGoSemantic(ctx context.Context, operation, rel string, source []byte, line, column int) (SemanticResult, error) {
	if _, err := m.EnsureFresh(ctx); err != nil && ctx.Err() != nil {
		return SemanticResult{}, err
	}
	record, ok := m.Snapshot().Files[rel]
	if !ok {
		return SemanticResult{}, fmt.Errorf("%s is not present in the syntax index", rel)
	}
	method := "lilith/go/" + operation
	switch operation {
	case "symbols", "document_symbols", "document-symbols":
		return SemanticResult{Server: "builtin-go", Method: method, Result: record.Symbols}, nil
	case "diagnostics", "diagnostic":
		return SemanticResult{Server: "builtin-go", Method: method, Result: goSyntaxDiagnostics(rel, source)}, nil
	}

	target, err := goSemanticTarget(record, source, line, column)
	if err != nil {
		return SemanticResult{}, err
	}
	query := firstNonEmpty(target.QualifiedName, target.Name)
	refs, defs, err := m.References(ctx, query, 500)
	if err != nil {
		return SemanticResult{}, err
	}
	switch operation {
	case "definition", "definitions":
		return SemanticResult{Server: "builtin-go", Method: method, Result: map[string]any{"target": target, "definitions": defs}}, nil
	case "references", "refs":
		return SemanticResult{Server: "builtin-go", Method: method, Result: map[string]any{"target": target, "definitions": defs, "references": refs}}, nil
	case "hover", "type", "info":
		declaration := target
		declarationSource := source
		if len(defs) > 0 {
			definition := defs[0]
			declaration = goSemanticLocation{
				Name: definition.Name, QualifiedName: definition.QualifiedName, Kind: definition.Kind,
				Path: definition.Path, StartLine: definition.StartLine, EndLine: definition.EndLine, Source: "definition",
			}
			if data, readErr := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(definition.Path))); readErr == nil {
				declarationSource = data
			}
		}
		return SemanticResult{Server: "builtin-go", Method: method, Result: map[string]any{
			"target":        target,
			"declaration":   declaration,
			"source_line":   sourceLine(declarationSource, declaration.StartLine),
			"scope":         "static syntax fallback; gopls may be installed separately for compiler-complete types",
			"type_complete": false,
		}}, nil
	default:
		return SemanticResult{}, fmt.Errorf("unsupported built-in Go semantic operation %q", operation)
	}
}

type goSemanticLocation struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	Source        string `json:"source,omitempty"`
}

func goSemanticTarget(record FileRecord, source []byte, line, column int) (goSemanticLocation, error) {
	if line <= 0 {
		line = 1
	}
	name := identifierAt(source, line, column)
	type candidate struct {
		location goSemanticLocation
		span     int
		exact    bool
		priority int
	}
	var candidates []candidate
	for _, symbol := range record.Symbols {
		if line < symbol.StartLine || line > symbol.EndLine {
			continue
		}
		candidates = append(candidates, candidate{
			location: goSemanticLocation{Name: symbol.Name, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Path: symbol.Path, StartLine: symbol.StartLine, EndLine: symbol.EndLine, Source: "definition"},
			span:     maxInt(symbol.EndLine-symbol.StartLine, 0),
			exact:    name != "" && symbol.Name == name,
			priority: 2,
		})
	}
	for _, ref := range record.References {
		if line < ref.StartLine || line > ref.EndLine {
			continue
		}
		candidates = append(candidates, candidate{
			location: goSemanticLocation{Name: ref.Name, QualifiedName: ref.QualifiedName, Kind: ref.Kind, Path: ref.Path, StartLine: ref.StartLine, EndLine: ref.EndLine, Source: "reference"},
			span:     maxInt(ref.EndLine-ref.StartLine, 0),
			exact:    name != "" && ref.Name == name,
			priority: 3,
		})
	}
	if len(candidates) == 0 && name != "" {
		for _, symbol := range record.Symbols {
			if symbol.Name == name {
				candidates = append(candidates, candidate{location: goSemanticLocation{Name: symbol.Name, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Path: symbol.Path, StartLine: symbol.StartLine, EndLine: symbol.EndLine, Source: "definition"}, span: symbol.EndLine - symbol.StartLine, exact: true, priority: 1})
			}
		}
	}
	if len(candidates) == 0 {
		return goSemanticLocation{}, fmt.Errorf("no indexed Go symbol or reference was found at %s:%d:%d", record.Path, line, column)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].exact != candidates[j].exact {
			return candidates[i].exact
		}
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		if candidates[i].span != candidates[j].span {
			return candidates[i].span < candidates[j].span
		}
		return candidates[i].location.StartLine < candidates[j].location.StartLine
	})
	return candidates[0].location, nil
}

func identifierAt(source []byte, line, column int) string {
	text := sourceLine(source, line)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	index := column - 1
	if index < 0 {
		index = 0
	}
	if index >= len(runes) {
		index = len(runes) - 1
	}
	if index < 0 {
		return ""
	}
	isIdent := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	if !isIdent(runes[index]) && index > 0 && isIdent(runes[index-1]) {
		index--
	}
	if !isIdent(runes[index]) {
		return ""
	}
	start, end := index, index+1
	for start > 0 && isIdent(runes[start-1]) {
		start--
	}
	for end < len(runes) && isIdent(runes[end]) {
		end++
	}
	return string(runes[start:end])
}

func sourceLine(source []byte, line int) string {
	if line <= 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

type goDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

func goSyntaxDiagnostics(path string, source []byte) []goDiagnostic {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, path, source, parser.AllErrors)
	if err == nil {
		return []goDiagnostic{}
	}
	var diagnostics []goDiagnostic
	switch list := err.(type) {
	case scanner.ErrorList:
		for _, item := range list {
			diagnostics = append(diagnostics, goDiagnostic{Severity: "error", Message: item.Msg, Line: item.Pos.Line, Column: item.Pos.Column})
		}
		return diagnostics
	case *scanner.ErrorList:
		for _, item := range *list {
			diagnostics = append(diagnostics, goDiagnostic{Severity: "error", Message: item.Msg, Line: item.Pos.Line, Column: item.Pos.Column})
		}
		return diagnostics
	}
	// Keep the result structured even for an unexpected parser error.
	line, column := 1, 1
	if !utf8.Valid(source) {
		return []goDiagnostic{{Severity: "error", Message: "source is not valid UTF-8", Line: line, Column: column}}
	}
	return []goDiagnostic{{Severity: "error", Message: err.Error(), Line: line, Column: column}}
}
