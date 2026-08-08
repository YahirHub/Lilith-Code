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

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/codeintel"
	ligoal "github.com/lilith/li/internal/goal"
	"github.com/lilith/li/internal/interaction"
	"github.com/lilith/li/internal/knowledge"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/skills"
	litodo "github.com/lilith/li/internal/todo"
)

// Env is the execution environment handed to every tool.
type Env struct {
	// Root is the project directory; every path is resolved inside it.
	Root string
	// CodeIntel is the repository-aware syntax/semantic engine shared by the session.
	CodeIntel *codeintel.Manager
	// ConfigDir is Lilith's global configuration directory (~/.li). Tools
	// that depend on user configuration (for example web_search) use it
	// without exposing secrets to the model.
	ConfigDir string
	// Materialize adds tool names to the active set (used by tool_search).
	Materialize func(names []string)
	// Skills is the already-discovered skill catalog for this session. Skill
	// tools never scan arbitrary user locations on their own.
	Skills []skills.Skill
	// Agents is the discovered Claude-compatible subagent catalog.
	Agents []agents.Agent
	// RunAgent starts/resumes one isolated subagent. The tools package owns only
	// the wire contract; the runtime callback is supplied by the chat/subagent
	// host to avoid coupling the registry to provider/session implementations.
	RunAgent func(ctx context.Context, req AgentRequest) (AgentResult, error)
	// Todos is the session-local authoritative task plan used by todo_write.
	Todos *litodo.Manager
	// Plan is the session-local Build/Plan/Goal mode state used by plan-specific tools.
	Plan *planstate.Manager
	// Goal is the durable Codex-style objective bound to this chat thread.
	Goal *ligoal.Manager
	// Knowledge is the process-wide read-only reference base. Its index remains
	// lazy and independent from project files and Agent Skills.
	Knowledge *knowledge.Base
	// MemoryDir is set only when persistent memory is enabled for the current
	// main/subagent context. Dedicated memory tools are hidden otherwise.
	MemoryDir string
	// AgentMode is the mode snapshotted for THIS turn. It intentionally differs
	// from Plan.Mode() when the user presses Tab while a turn is running.
	AgentMode planstate.Mode
	// ToolVisible is an optional capability filter applied consistently to lazy
	// selection, tool_search materialization and direct execution. Plan mode uses
	// it as a hard read-only ceiling rather than relying on prompt obedience.
	ToolVisible func(name string, def Definition) bool
	// BeforeTool/AfterTool host Claude-compatible lifecycle hooks without
	// coupling the registry to settings or the TUI. BeforeTool may replace args.
	BeforeTool func(ctx context.Context, name string, args map[string]any) (map[string]any, error)
	AfterTool  func(ctx context.Context, name string, args map[string]any, output string, runErr error) (string, error)
	// ValidateTool performs argument-aware runtime policy checks immediately
	// before execution (for example the Plan-mode shell allowlist).
	ValidateTool func(name string, def Definition, args map[string]any) error
	// RequestSecret asks the local TUI for a masked value. The value bypasses the
	// model, tool arguments, transcript and session persistence.
	RequestSecret func(ctx context.Context, secretKind interaction.SecretKind, title, message string, confirm bool, minLength int) (string, error)
	// Confirm asks the local user before a sensitive action.
	Confirm func(ctx context.Context, title, message string) (bool, error)
	// Approve exposes scoped decisions for SSH: once, this process session, or
	// always for the current project. Other confirmation flows keep using Confirm.
	Approve func(ctx context.Context, title, message, approvalKey string) (interaction.ApprovalDecision, error)
	// DynamicTool executes schemas supplied outside the static registry (MCP).
	// It is consulted only for unknown names, keeping built-in policy explicit.
	DynamicTool func(ctx context.Context, name string, args map[string]any) (string, error)
}

// Definition describes one callable tool.
type Definition struct {
	Name        string
	Description string
	// PromptSnippet is the short one-line description included in the system
	// prompt when this tool is active. The full Description already travels in
	// the tool schema, so keeping this short avoids paying for the same text twice.
	PromptSnippet string
	// PromptGuidelines are concise usage rules that only enter the prompt while
	// this tool is active. This mirrors pi.dev's promptSnippet/promptGuidelines
	// pattern and keeps lazy tool selection useful for prompt tokens too.
	PromptGuidelines []string
	// Parameters is a JSON Schema object.
	Parameters any
	// Mutating marks tools that write files or run commands.
	Mutating bool
	// Available dynamically hides a tool when its prerequisites are not met.
	// A hidden tool is omitted from lazy discovery and rejected if a model
	// nevertheless tries to call it. Nil means always available.
	Available func(env Env) bool
	Run       func(ctx context.Context, args map[string]any, env Env) (string, error)
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

func availableInEnv(name string, d Definition, env Env) bool {
	if d.Available != nil && !d.Available(env) {
		return false
	}
	if env.ToolVisible != nil && !env.ToolVisible(name, d) {
		return false
	}
	return true
}

// PromptInfo returns compact prompt metadata for the active tool set. Unknown
// tools and empty snippets are ignored; guidelines are de-duplicated while
// preserving tool order.
func PromptInfo(names []string) (toolLines []string, guidelines []string) {
	seenGuideline := map[string]bool{}
	for _, name := range names {
		d, ok := registry[name]
		if !ok {
			continue
		}
		if snippet := strings.TrimSpace(d.PromptSnippet); snippet != "" {
			toolLines = append(toolLines, name+": "+snippet)
		}
		for _, guideline := range d.PromptGuidelines {
			g := strings.TrimSpace(guideline)
			if g == "" || seenGuideline[g] {
				continue
			}
			seenGuideline[g] = true
			guidelines = append(guidelines, g)
		}
	}
	return toolLines, guidelines
}

// Schema is the OpenAI-compatible wire shape of a tool.
type Schema struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
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
	if args == nil {
		args = map[string]any{}
	}
	// Compatibility guard, intentionally NOT registered as a schema. Models
	// trained on other coding agents occasionally hallucinate the ambiguous legacy
	// name `write`. Lilith exposes explicit write_file/append_file tools, so only
	// the unsupported alias is intercepted.
	if name == "write" {
		if env.AgentMode == planstate.Plan {
			return "", fmt.Errorf("Plan mode is read-only: tool %s is blocked until you switch back to Build with Tab", name)
		}
		return InterceptLegacyWrite(env.Root, name, str(args, "path"))
	}
	// Compatibility aliases for models trained on Claude/Codebuff/Pi naming.
	switch name {
	case "TodoWrite", "todowrite", "write_todos", "todo":
		name = "todo_write"
	case "Task", "task", "agent":
		name = "Agent"
	}
	d, ok := registry[name]
	if !ok {
		if env.DynamicTool != nil && strings.HasPrefix(name, "mcp__") {
			if env.BeforeTool != nil {
				updated, err := env.BeforeTool(ctx, name, args)
				if err != nil {
					return "", err
				}
				if updated != nil {
					args = updated
				}
			}
			out, runErr := env.DynamicTool(ctx, name, args)
			if env.AfterTool != nil {
				updated, hookErr := env.AfterTool(ctx, name, args, out, runErr)
				if hookErr != nil {
					return updated, hookErr
				}
				out = updated
			}
			return out, runErr
		}
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if !availableInEnv(name, d, env) {
		return "", fmt.Errorf("tool unavailable: %s", name)
	}
	if env.BeforeTool != nil {
		updated, err := env.BeforeTool(ctx, name, args)
		if err != nil {
			return "", err
		}
		if updated != nil {
			args = updated
		}
	}
	if env.ValidateTool != nil {
		if err := env.ValidateTool(name, d, args); err != nil {
			return "", err
		}
	}
	out, runErr := d.Run(ctx, args, env)
	if env.AfterTool != nil {
		updated, hookErr := env.AfterTool(ctx, name, args, out, runErr)
		if hookErr != nil {
			return updated, hookErr
		}
		out = updated
	}
	return out, runErr
}

// -----------------------------------------------------------------------------
// Lazy selection (portado de original/packages/agent-runtime/src/tools/lazy-tool-selection.ts)
// -----------------------------------------------------------------------------

// alwaysOn is the microscopic unconditional surface: the model can always
// discover the rest with tool_search, so we never pay for unused schemas.
var alwaysOn = []string{"tool_search", "todo_write", "plan_question", "plan_exit", "Agent"}

var directChatPattern = regexp.MustCompile(`(?i)^\s*(hola|hello|hi|hey|buenas( (dias|días|tardes|noches))?|gracias|thanks|thank you|ok(ay)?|vale|entendido|perfecto|listo|adios|adiós|bye)[!.?…\s]*$`)

var (
	projectScopePattern  = regexp.MustCompile(`(?i)\b(project|proyecto|repo|repositor(y|io)|codebase|c(o|ó)digo|file|archivo|files|archivos|src|carpeta|directorio|package\.json|go\.mod|cargo\.toml|pyproject\.toml)\b`)
	filePathPattern      = regexp.MustCompile(`(?i)[\w.-]+\.(ts|tsx|js|jsx|mjs|cjs|go|rs|py|java|kt|php|json|ya?ml|toml|md|css|scss|html|vue|svelte|txt|sh)\b`)
	imagePathPattern     = regexp.MustCompile(`(?i)([\w .()\-]+\.(png|jpe?g|gif|bmp|tiff?|webp)\b|\b(imagen|image|captura|screenshot|ocr|mockup|maqueta|interfaz|ui)\b)`)
	createFilePattern    = regexp.MustCompile(`(?i)\b(crea|crear|create|genera|generar|generate)\b\s+(?:(un|una|a|the)\s+)?(?:(nuevo|nueva|new)\s+)?(archivo|file|fichero)\b`)
	newFilePattern       = regexp.MustCompile(`(?i)(\b(nuevo|nueva|new)\s+(archivo|file|fichero)\b|\b(agrega|añade|add)\b.{0,24}\b(archivo|file|fichero)\b)`)
	writePattern         = regexp.MustCompile(`(?i)\b(escribe|write|crea|crear|create|genera|generar|generate|implementa|implement|agrega|añade|add|modifica|modify|edita|edit|corrige|fix|refactoriza|refactor|renombra|rename|elimina|borra|delete|remove|guarda|save|haz|hazme|dame)\b`)
	fullFileWritePattern = regexp.MustCompile(`(?i)\b(reporte|report|documento|document|markdown|readme|archivo completo|full file|reescribe|rewrite|regenera|regenerate|sobrescribe|overwrite)\b`)
	appendFilePattern    = regexp.MustCompile(`(?i)\b(append|anexa|anexar|concatena|concatenar|por secciones|section by section|reporte|report)\b`)
	createSearchPattern  = regexp.MustCompile(`(?i)\b(create|new|crear|nuevo|nueva|generar|genera|add new|agrega nuevo|añade nuevo)\b`)
	searchPattern        = regexp.MustCompile(`(?i)\b(busca|search|encuentra|find|grep|ripgrep|rg|d(o|ó)nde|where|localiza|usages|referencias)\b`)
	symbolPattern        = regexp.MustCompile(`(?i)\b(s(i|í)mbolo|symbol|funci(o|ó)n|function|m(e|é)todo|method|clase|class|struct|interface|definition|definici(o|ó)n|callers?|llamadas?)\b`)
	semanticPattern      = regexp.MustCompile(`(?i)\b(lsp|language server|semantic|sem(a|á)ntic|tipos?|types?|diagn(o|ó)stic|hover|go to definition|referencias exactas)\b`)
	validationPattern    = regexp.MustCompile(`(?i)\b(valida|validate|lint|formatter|format|typecheck|compila|compile|build|tests?|pruebas?|go vet|cargo check)\b`)
	scipPattern          = regexp.MustCompile(`(?i)\b(scip|semantic index|indice sem(a|á)ntico|índice sem(a|á)ntico)\b`)
	shellPattern         = regexp.MustCompile(`(?i)\b(ejecuta|execute|run|comando|command|terminal|bash|shell|portable|embebid[ao]|posix|sin bash|without bash|compila|compile|build|test|prueba|git|github|gh|docker|compose|npm|go run|instala|install)\b`)
	urlPattern           = regexp.MustCompile(`(?i)(https?://|\b(url|web|p(a|á)gina|docs? online|documentaci(o|ó)n online)\b)`)
	webSearchPattern     = regexp.MustCompile(`(?i)\b(internet|web|online|actualizado|actualizada|reciente|latest|current|news|noticias|hoy|today)\b|última\s+versión|ultima\s+version|latest\s+version`)
	sshPattern           = regexp.MustCompile(`(?i)\b(ssh|sftp|servidor remoto|servidores remotos|remote server|remote host|b[oó]veda|vault|contrase[nñ]a ssh|clave privada|private key|deploy remoto|despliegue remoto)\b`)
	archivePattern       = regexp.MustCompile(`(?i)\b(gitzip|zip|tar(?:\.gz)?|archivo comprimido|comprimir|empaquetar|archive|package|subir proyecto|upload project)\b`)
	browserPattern       = regexp.MustCompile(`(?i)\b(chromedp|chrome|chromium|navegador|browser|devtools|dom|consola (?:del )?navegador|network (?:del )?navegador|p[aá]gina web|iniciar sesi[oó]n|login web|screenshot web|captura web|frontend visual|audita(?:r)? frontend|revisa(?:r)? (?:todas )?las p[aá]ginas|errores? de consola|rutas web)\b`)
)

var promptHints = []struct {
	pattern *regexp.Regexp
	tools   []string
}{
	{projectScopePattern, []string{"code_intel_status", "code_graph", "code_context", "list_directory", "glob", "read_files"}},
	{filePathPattern, []string{"code_context", "read_files", "str_replace", "apply_diff"}},
	{imagePathPattern, []string{"extract_image_text", "read_files"}},
	{writePattern, []string{"code_context", "code_validate", "str_replace", "apply_diff", "read_files"}},
	{fullFileWritePattern, []string{"write_file"}},
	{appendFilePattern, []string{"append_file"}},
	{searchPattern, []string{"code_symbols", "code_references", "code_graph", "code_context", "code_search", "glob", "read_files"}},
	{symbolPattern, []string{"code_symbols", "code_references", "code_graph", "code_context"}},
	{semanticPattern, []string{"code_semantic", "code_symbols", "code_references"}},
	{validationPattern, []string{"code_validate"}},
	{scipPattern, []string{"code_scip_search"}},
	{shellPattern, []string{"run_terminal_command"}},
	{urlPattern, []string{"read_url"}},
	{webSearchPattern, []string{"web_search"}},
	{sshPattern, []string{"ssh_remote"}},
	{archivePattern, []string{"gitzip"}},
	{browserPattern, []string{"browser"}},
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
	// create_file remains narrower than the generic write/edit surface because it
	// is creation-only. write_file is safe to expose broadly: existing targets
	// still require an explicit overwrite=true argument.
	// If a broader implementation later needs a new file, tool_search can load it.
	if createFilePattern.MatchString(p) || newFilePattern.MatchString(p) {
		if _, ok := registry["create_file"]; ok {
			active["create_file"] = true
		}
	}
	out := make([]string, 0, len(active))
	for n := range active {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SelectAvailable applies the regular lazy selection and removes tools whose
// runtime prerequisites are not satisfied. This is the path used by the chat.
func SelectAvailable(prompt string, env Env) []string {
	names := Select(prompt)
	out := make([]string, 0, len(names))
	for _, name := range names {
		d, ok := registry[name]
		if !ok || !availableInEnv(name, d, env) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// FilterAvailable defensively removes tools that became unavailable after the
// turn started (for example because a search credential was disabled).
func FilterAvailable(names []string, env Env) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		d, ok := registry[name]
		if !ok || !availableInEnv(name, d, env) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// WithSkillTools keeps the small skill-navigation surface available whenever
// model-visible skills exist. Pi can rely on its generic read tool being always
// present; Lilith uses dedicated skill tools, so hiding them behind tool_search
// makes automatic skill activation needlessly easy for models to skip.
func WithSkillTools(names []string, hasSkills bool) []string {
	if !hasSkills {
		return names
	}
	active := make(map[string]bool, len(names)+3)
	for _, name := range names {
		if _, ok := registry[name]; ok {
			active[name] = true
		}
	}
	for _, name := range []string{"skill_read", "skill_search", "skill_files"} {
		if _, ok := registry[name]; ok {
			active[name] = true
		}
	}
	out := make([]string, 0, len(active))
	for name := range active {
		out = append(out, name)
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
			"Use this when you need to read, write, append or search project files (including search without ripgrep), inspect/search skills, consult local Knowledge references, run native or portable-shell commands (including Git/GitHub CLI and Docker when installed), control a browser, or fetch a URL " +
			"and that tool is not loaded yet.",
		PromptSnippet: "Discover and enable additional tools on demand",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Keywords, e.g. \"write file\", \"append report\", \"search code\", \"portable shell\" or \"run command\".",
				},
			},
			"required": []string{"query"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			query := strings.ToLower(str(args, "query"))
			var matches []string
			var b strings.Builder
			allowCreate := createSearchPattern.MatchString(query)
			skillQuery := strings.Contains(query, "skill")
			for _, n := range order {
				if n == "tool_search" {
					continue
				}
				// Generic queries such as "write file" or "edit css" must not
				// materialize create_file. Models strongly associate full-file write
				// tools with overwriting; require explicit creation intent instead.
				if n == "create_file" && !allowCreate {
					continue
				}
				d := registry[n]
				if !availableInEnv(n, d, env) {
					continue
				}
				hay := strings.ToLower(d.Name + " " + d.Description)
				if skillQuery && !strings.Contains(hay, "skill") {
					continue
				}
				if query != "" && !containsAnyWord(hay, query) {
					continue
				}
				matches = append(matches, n)
				fmt.Fprintf(&b, "- %s: %s\n", d.Name, d.Description)
			}
			if len(matches) == 0 {
				// Sin coincidencias devolvemos el catálogo seguro. create_file sólo se
				// materializa cuando la propia búsqueda expresa intención de crear.
				for _, n := range order {
					if n == "tool_search" || (n == "create_file" && !allowCreate) {
						continue
					}
					d := registry[n]
					if !availableInEnv(n, d, env) {
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
