package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lilith/li/internal/codeintel"
)

func init() {
	available := func(env Env) bool { return env.CodeIntel != nil }
	register(Definition{
		Name:          "code_intel_status",
		Description:   "Detect the current operating system, shell, project ecosystems, package manager, installed language servers, SCIP index, physical index path and persistent syntax-index status. Optionally refresh the index incrementally.",
		PromptSnippet: "Inspect detected host, project languages and code-intelligence capabilities",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"refresh": map[string]any{"type": "boolean", "description": "Incrementally refresh the persistent syntax index before reporting."},
		}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			return env.CodeIntel.StatusText(ctx, boolArg(args, "refresh"))
		},
	})
	register(Definition{
		Name:             "code_symbols",
		Description:      "Find declarations from Lilith's persistent embedded syntax index. Go uses Tree-sitter plus go/ast for precise functions, methods, types, constants and variables; other languages use Tree-sitter/fallback. Search by symbol or qualified name and optionally filter by path or kind without reading complete files.",
		PromptSnippet:    "Find functions, classes, methods, structs, types, constants, variables and other declarations",
		PromptGuidelines: []string{"Prefer code_symbols or code_context over broad reads when locating an implementation."},
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Fuzzy symbol name; empty lists top indexed declarations."},
			"path":  map[string]any{"type": "string", "description": "Optional path substring filter."},
			"kind":  map[string]any{"type": "string", "description": "Optional kind such as function, method, class, struct, interface, constant or variable."},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result, err := env.CodeIntel.Symbols(ctx, str(args, "query"), str(args, "path"), str(args, "kind"), intArg(args, "limit", 100))
			return prettyJSON(result), err
		},
	})
	register(Definition{
		Name:          "code_references",
		Description:   "Resolve syntax-level definitions and call/reference sites across the repository. Go references use canonical package-qualified identities and import aliases to avoid same-name false positives; use code_semantic for compiler-accurate references when a language server is installed.",
		PromptSnippet: "Find definitions and call sites for a symbol",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"name":  map[string]any{"type": "string", "description": "Symbol or qualified symbol name."},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		}, "required": []string{"name"}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			refs, defs, err := env.CodeIntel.References(ctx, str(args, "name"), intArg(args, "limit", 200))
			if err != nil {
				return "", err
			}
			return prettyJSON(map[string]any{"definitions": defs, "references": refs}), nil
		},
	})
	register(Definition{
		Name:          "code_graph",
		Description:   "Build a connected, task-ranked repository graph of files, packages, modules, declarations, imports, calls, references and test relationships. Query seeds expand through adjacent nodes so relevant execution flow is preserved.",
		PromptSnippet: "Map repository dependencies, calls and important files",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Optional task, component or symbol used for ranking."},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result, err := env.CodeIntel.Graph(ctx, str(args, "query"), intArg(args, "limit", 120))
			return prettyJSON(result), err
		},
	})
	register(Definition{
		Name:             "code_context",
		Description:      "Select compact, ranked, syntax-bounded code snippets related to a task or symbol. Ranking considers multilingual task terms, declarations, canonical references, production-vs-test paths and Git-changed files while suppressing unrelated documentation and release scripts.",
		PromptSnippet:    "Build a compact repository map and AST-bounded context for a task",
		PromptGuidelines: []string{"Use code_context before editing unfamiliar code; inspect exact files afterward only when needed."},
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query":     map[string]any{"type": "string", "description": "Task, error, component or symbol to rank."},
			"paths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional path substrings."},
			"max_chars": map[string]any{"type": "integer", "minimum": 1000, "maximum": 120000},
		}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			chunks, err := env.CodeIntel.Context(ctx, str(args, "query"), strSlice(args, "paths"), intArg(args, "max_chars", 24000))
			if err != nil {
				return "", err
			}
			return formatContextChunks(chunks), nil
		},
	})
	register(Definition{
		Name:             "code_validate",
		Description:      "Run project-aware compiler, linter and test commands selected from real manifests. It never installs dependencies or intentionally edits source files, but project tools may update caches or build outputs.",
		PromptSnippet:    "Validate changed code with the project's real compiler, linter and tests",
		PromptGuidelines: []string{"After code edits, run code_validate with changed_paths before broad test commands."},
		Parameters:       validationSchema(false),
		Mutating:         true,
		Available:        available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result := env.CodeIntel.Validate(ctx, validationOptions(args, false))
			return prettyJSON(result), nil
		},
	})
	register(Definition{
		Name:             "code_format_validate",
		Description:      "Explicitly apply installed project formatters to changed files, then run project-aware compiler/lint/test checks. This mutates files and never installs dependencies.",
		PromptSnippet:    "Format changed files and validate them with project-native tools",
		PromptGuidelines: []string{"Use code_format_validate only when formatting changes are intended; review its changed files afterward."},
		Parameters:       validationSchema(true),
		Mutating:         true,
		Available:        available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result := env.CodeIntel.Validate(ctx, validationOptions(args, true))
			return prettyJSON(result), nil
		},
	})
	register(Definition{
		Name:          "code_semantic",
		Description:   "Query an installed Language Server Protocol server for document symbols, definitions, references, hover/type information or diagnostics. Lilith never installs or embeds gopls or another language server. When Go has no gopls, a fully static built-in fallback provides indexed definitions/references, declaration hover and parser diagnostics without weakening CGO_ENABLED=0 builds.",
		PromptSnippet: "Use an installed language server or Lilith's static Go semantic fallback",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"operation": map[string]any{"type": "string", "enum": []string{"symbols", "definition", "references", "hover", "diagnostics"}},
			"path":      map[string]any{"type": "string"},
			"line":      map[string]any{"type": "integer", "minimum": 1},
			"column":    map[string]any{"type": "integer", "minimum": 1},
		}, "required": []string{"operation", "path"}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result, err := env.CodeIntel.Semantic(ctx, codeintel.SemanticRequest{Operation: str(args, "operation"), Path: str(args, "path"), Line: intArg(args, "line", 1), Column: intArg(args, "column", 1)})
			return prettyJSON(result), err
		},
	})
	register(Definition{
		Name:          "code_scip_search",
		Description:   "Search an existing SCIP semantic index for very large repositories using an installed scip CLI. It never creates or downloads an index.",
		PromptSnippet: "Search an existing SCIP index in a large repository",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
		}, "required": []string{"query"}},
		Available: available,
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			result, err := env.CodeIntel.SCIPSearch(ctx, str(args, "query"), intArg(args, "limit", 40))
			return prettyJSON(result), err
		},
	})
}

func validationSchema(format bool) map[string]any {
	description := "Paths changed in this task; used to limit quick checks."
	if format {
		description = "Paths to format and validate. Empty means validation only because formatters are never applied repository-wide implicitly."
	}
	return map[string]any{"type": "object", "properties": map[string]any{
		"changed_paths":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description},
		"mode":            map[string]any{"type": "string", "enum": []string{"quick", "full"}, "description": "Quick checks affected code; full runs project suites when available."},
		"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 3600},
	}}
}

func validationOptions(args map[string]any, apply bool) codeintel.ValidationOptions {
	seconds := intArg(args, "timeout_seconds", 600)
	if seconds < 1 {
		seconds = 600
	}
	return codeintel.ValidationOptions{ChangedPaths: strSlice(args, "changed_paths"), Mode: str(args, "mode"), ApplyFormat: apply, Timeout: time.Duration(seconds) * time.Second}
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func formatContextChunks(chunks []codeintel.ContextChunk) string {
	if len(chunks) == 0 {
		return "No relevant syntax-bounded context was found."
	}
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "\n### %s", chunk.Path)
		if chunk.Symbol != "" {
			fmt.Fprintf(&b, " — %s %s", chunk.Kind, chunk.Symbol)
		}
		fmt.Fprintf(&b, " (lines %d-%d, score %d)\n```%s\n%s```\n", chunk.StartLine, chunk.EndLine, chunk.Score, fenceLanguage(chunk.Language), chunk.Content)
	}
	return strings.TrimSpace(b.String())
}

func fenceLanguage(language string) string {
	switch language {
	case "shell":
		return "bash"
	case "csharp":
		return "csharp"
	case "cpp":
		return "cpp"
	default:
		return language
	}
}
