// Package tools implements the tool-call surface of Lilith: a small registry
// of file, search, shell and web tools plus the lazy selection logic that
// keeps prompt token usage minimal (only the tools a prompt actually needs
// are sent to the model; the rest are discoverable through `tool_search`).
package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Env is the execution environment handed to every tool.
type Env struct {
	// Root is the project directory; every path is resolved inside it.
	Root string
	// Materialize adds tool names to the active set (used by tool_search).
	Materialize func(names []string)
	// Seen marks the files already read (o escritos) en esta sesión. Sirve
	// para impedir que el modelo sobrescriba a ciegas un archivo existente
	// en lugar de editarlo con str_replace.
	Seen map[string]bool
}

// Definition describes one callable tool.
type Definition struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object.
	Parameters map[string]any
	// Mutating marks tools that write files or run commands.
	Mutating bool
	Run      func(ctx context.Context, args map[string]any, env Env) (string, error)
}

var registry = map[string]Definition{}
var order []string

func register(d Definition) {
	registry[d.Name] = d
	order = append(order, d.Name)
	sort.Strings(order)
}

// Get returns a tool definition by name.
func Get(name string) (Definition, bool) {
	d, ok := registry[name]
	return d, ok
}

// Names returns every registered tool name, sorted.
func Names() []string { return append([]string(nil), order...) }

// Schema is the OpenAI-compatible wire shape of a tool.
type Schema struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Schemas converts the given tool names into wire schemas, skipping unknowns.
func Schemas(names []string) []Schema {
	out := make([]Schema, 0, len(names))
	for _, n := range names {
		d, ok := registry[n]
		if !ok {
			continue
		}
		var s Schema
		s.Type = "function"
		s.Function.Name = d.Name
		s.Function.Description = d.Description
		s.Function.Parameters = d.Parameters
		out = append(out, s)
	}
	return out
}

// Execute runs a tool by name with decoded arguments.
func Execute(ctx context.Context, name string, args map[string]any, env Env) (string, error) {
	d, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	return d.Run(ctx, args, env)
}

// -----------------------------------------------------------------------------
// Lazy selection (portado de original/packages/agent-runtime/src/tools/lazy-tool-selection.ts)
// -----------------------------------------------------------------------------

// alwaysOn is the microscopic unconditional surface: the model can always
// discover the rest with tool_search, so we never pay for unused schemas.
var alwaysOn = []string{"tool_search"}

var directChatPattern = regexp.MustCompile(`(?i)^\s*(hola|hello|hi|hey|buenas( (dias|días|tardes|noches))?|gracias|thanks|thank you|ok(ay)?|vale|entendido|perfecto|listo|adios|adiós|bye)[!.?…\s]*$`)

var (
	projectScopePattern = regexp.MustCompile(`(?i)\b(project|proyecto|repo|repositor(y|io)|codebase|c(o|ó)digo|file|archivo|files|archivos|src|carpeta|directorio|package\.json|go\.mod|cargo\.toml|pyproject\.toml)\b`)
	filePathPattern     = regexp.MustCompile(`(?i)[\w.-]+\.(ts|tsx|js|jsx|mjs|cjs|go|rs|py|java|kt|php|json|ya?ml|toml|md|css|scss|html|vue|svelte|txt|sh)\b`)
	writePattern        = regexp.MustCompile(`(?i)\b(crea|create|escribe|write|genera|generate|implementa|implement|agrega|añade|add|modifica|modify|edita|edit|corrige|fix|refactoriza|refactor|renombra|rename|elimina|borra|delete|remove|guarda|save|haz|hazme|dame)\b`)
	searchPattern       = regexp.MustCompile(`(?i)\b(busca|search|encuentra|find|grep|d(o|ó)nde|where|localiza|usages|referencias)\b`)
	shellPattern        = regexp.MustCompile(`(?i)\b(ejecuta|execute|run|comando|command|terminal|bash|shell|compila|compile|build|test|prueba|git|npm|go run|instala|install)\b`)
	urlPattern          = regexp.MustCompile(`(?i)(https?://|\b(url|web|p(a|á)gina|docs? online|documentaci(o|ó)n online)\b)`)
)

var promptHints = []struct {
	pattern *regexp.Regexp
	tools   []string
}{
	{projectScopePattern, []string{"list_directory", "glob", "read_files"}},
	{filePathPattern, []string{"read_files", "write_file", "str_replace", "apply_diff"}},
	{writePattern, []string{"write_file", "str_replace", "read_files"}},
	{searchPattern, []string{"code_search", "glob", "read_files"}},
	{shellPattern, []string{"run_terminal_command"}},
	{urlPattern, []string{"read_url"}},
}

// IsDirectChat reports a pure greeting/acknowledgement: no schemas at all.
func IsDirectChat(prompt string) bool {
	return directChatPattern.MatchString(prompt)
}

// Select returns the initial active tool names for a prompt.
func Select(prompt string) []string {
	p := strings.TrimSpace(prompt)
	if p == "" || IsDirectChat(p) {
		return nil
	}
	active := map[string]bool{}
	for _, n := range alwaysOn {
		active[n] = true
	}
	for _, h := range promptHints {
		if !h.pattern.MatchString(p) {
			continue
		}
		for _, t := range h.tools {
			if _, ok := registry[t]; ok {
				active[t] = true
			}
		}
	}
	out := make([]string, 0, len(active))
	for n := range active {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// -----------------------------------------------------------------------------
// tool_search: la meta-herramienta que materializa el resto bajo demanda.
// -----------------------------------------------------------------------------

func init() {
	register(Definition{
		Name: "tool_search",
		Description: "Search available tools by keyword and enable them for the next calls. " +
			"Use this when you need to read, write or search files, run commands, or fetch a URL " +
			"and that tool is not loaded yet.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Keywords, e.g. \"write file\" or \"run command\".",
				},
			},
			"required": []string{"query"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			query := strings.ToLower(str(args, "query"))
			var matches []string
			var b strings.Builder
			for _, n := range order {
				if n == "tool_search" {
					continue
				}
				d := registry[n]
				hay := strings.ToLower(d.Name + " " + d.Description)
				if query != "" && !containsAnyWord(hay, query) {
					continue
				}
				matches = append(matches, n)
				fmt.Fprintf(&b, "- %s: %s\n", d.Name, d.Description)
			}
			if len(matches) == 0 {
				// Sin coincidencias devolvemos el catálogo completo (es corto).
				for _, n := range order {
					if n == "tool_search" {
						continue
					}
					matches = append(matches, n)
					fmt.Fprintf(&b, "- %s: %s\n", n, registry[n].Description)
				}
			}
			if env.Materialize != nil {
				env.Materialize(matches)
			}
			return "tools enabled for the next calls:\n" + b.String(), nil
		},
	})
}

func containsAnyWord(haystack, query string) bool {
	for _, w := range strings.Fields(query) {
		if len(w) < 3 {
			continue
		}
		if strings.Contains(haystack, w) {
			return true
		}
	}
	return false
}

func str(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		if v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}
