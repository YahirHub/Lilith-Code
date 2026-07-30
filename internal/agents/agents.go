// Package agents discovers Claude-compatible subagent definitions and exposes
// their metadata without loading a subagent's full prompt into the main
// conversation. Definitions are Markdown files with YAML frontmatter.
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Agent is a portable subagent definition. Claude fields are kept by their
// canonical names; a small subset of OpenCode/Pi fields is accepted as aliases
// so the same Markdown file can usually be shared unchanged.
type Agent struct {
	Name            string
	Description     string
	Prompt          string
	Tools           []string
	DisallowedTools []string
	Model           string
	PermissionMode  string
	MaxTurns        int
	Skills          []string
	Mode            string // subagent | all | primary (OpenCode compatibility)
	Hidden          bool
	Background      bool
	BackgroundSet   bool
	Isolation       string
	Color           string
	Memory          string
	Effort          string
	InitialPrompt   string
	MCPServers      []string
	MCPRaw          string
	HooksRaw        string
	Permissions     map[string]string // OpenCode permission shorthand
	ToolFlags       map[string]bool   // OpenCode legacy tools: {name: true|false}
	Source          string
	FilePath        string
	BaseDir         string
	PluginRoot      string
}

// LoadOptions controls discovery. Directories are processed from low to high
// precedence; later definitions with the same name replace earlier ones.
type LoadOptions struct {
	BuiltinDir  string
	UserDirs    []string
	ProjectDirs []string
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// DefaultLoadOptions deliberately treats Claude's layout as the canonical
// portable format, while also reading Pi/OpenCode/OpenClaude and Lilith-native
// locations. Within each scope, ~/.li and .li have the highest precedence.
func DefaultLoadOptions(configDir, projectRoot string) LoadOptions {
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	var user []string
	if home != "" {
		user = append(user,
			filepath.Join(home, ".pi", "agent", "agents"),
			filepath.Join(home, ".pi", "agents"),
			filepath.Join(home, ".config", "opencode", "agents"),
			filepath.Join(home, ".openclaude", "agents"),
			filepath.Join(home, ".agents", "agents"),
			filepath.Join(home, ".claude", "agents"),
			filepath.Join(home, ".li", "agents"),
		)
	}
	var project []string
	for _, root := range projectSearchRoots(projectRoot) {
		project = append(project,
			filepath.Join(root, ".pi", "agents"),
			filepath.Join(root, ".opencode", "agents"),
			filepath.Join(root, ".openclaude", "agents"),
			filepath.Join(root, ".agents", "agents"),
			filepath.Join(root, ".claude", "agents"),
			filepath.Join(root, ".li", "agents"),
		)
	}
	return LoadOptions{BuiltinDir: BundledDir(configDir), UserDirs: user, ProjectDirs: project}
}

// projectSearchRoots mirrors Claude/OpenCode discovery closely enough for a CLI
// started from a nested directory: search from the repository root toward the
// current directory so nearer definitions win by normal precedence.
func projectSearchRoots(projectRoot string) []string {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil
	}
	cur := filepath.Clean(projectRoot)
	var roots []string
	for {
		roots = append(roots, cur)
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	for i, j := 0, len(roots)-1; i < j; i, j = i+1, j-1 {
		roots[i], roots[j] = roots[j], roots[i]
	}
	return roots
}

// Load recursively discovers Markdown definitions. Project > user > builtin.
func Load(opts LoadOptions) []Agent {
	seen := map[string]Agent{}
	if strings.TrimSpace(opts.BuiltinDir) != "" {
		scan(opts.BuiltinDir, "builtin", seen)
	}
	for _, dir := range opts.UserDirs {
		scan(dir, "user", seen)
	}
	for _, dir := range opts.ProjectDirs {
		scan(dir, "project", seen)
	}
	out := make([]Agent, 0, len(seen))
	for _, a := range seen {
		// Primary-only OpenCode agents are not subagents. "all" remains usable.
		if strings.EqualFold(a.Mode, "primary") {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func scan(dir, source string, out map[string]Agent) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		if a, ok := loadFile(path, source); ok {
			out[strings.ToLower(a.Name)] = a
		}
		return nil
	})
}

func loadFile(path, source string) (Agent, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, false
	}
	fm, body := parseDocument(string(data))
	name := cleanScalar(fm.scalars["name"])
	if name == "" {
		// OpenCode agents commonly use the filename as identity.
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	desc := cleanScalar(fm.scalars["description"])
	if name == "" || desc == "" || !nameRe.MatchString(name) {
		return Agent{}, false
	}
	maxTurns := parseInt(firstNonEmpty(fm.scalars["maxturns"], fm.scalars["max_turns"], fm.scalars["steps"]))
	mode := strings.ToLower(cleanScalar(fm.scalars["mode"]))
	if mode == "" {
		mode = "subagent"
	}
	return Agent{
		Name:            name,
		Description:     desc,
		Prompt:          strings.TrimSpace(body),
		Tools:           listValue(fm, "tools"),
		DisallowedTools: appendUnique(nil, append(listValue(fm, "disallowedtools"), listValue(fm, "disallowed_tools")...)...),
		Model:           cleanScalar(fm.scalars["model"]),
		PermissionMode:  cleanScalar(firstNonEmpty(fm.scalars["permissionmode"], fm.scalars["permission_mode"])),
		MaxTurns:        maxTurns,
		Skills:          listValue(fm, "skills"),
		Mode:            mode,
		Hidden:          parseBool(fm.scalars["hidden"]),
		Background:      parseBool(fm.scalars["background"]),
		BackgroundSet:   strings.TrimSpace(fm.scalars["background"]) != "",
		Isolation:       cleanScalar(fm.scalars["isolation"]),
		Color:           cleanScalar(fm.scalars["color"]),
		Memory:          strings.ToLower(cleanScalar(fm.scalars["memory"])),
		Effort:          strings.ToLower(cleanScalar(fm.scalars["effort"])),
		InitialPrompt:   cleanScalar(firstNonEmpty(fm.scalars["initialprompt"], fm.scalars["initial_prompt"])),
		MCPServers:      appendUnique(nil, append(listValue(fm, "mcpservers"), listValue(fm, "mcp_servers")...)...),
		MCPRaw:          strings.TrimSpace(firstNonEmpty(fm.blocks["mcpservers"], fm.blocks["mcp_servers"])),
		HooksRaw:        strings.TrimSpace(fm.blocks["hooks"]),
		Permissions:     fm.permission,
		ToolFlags:       fm.toolFlags,
		Source:          source,
		FilePath:        path,
		BaseDir:         filepath.Dir(path),
	}, true
}

type frontmatter struct {
	scalars    map[string]string
	lists      map[string][]string
	permission map[string]string
	toolFlags  map[string]bool
	blocks     map[string]string
}

func parseDocument(content string) (frontmatter, string) {
	fm := frontmatter{scalars: map[string]string{}, lists: map[string][]string{}, permission: map[string]string{}, toolFlags: map[string]bool{}, blocks: map[string]string{}}
	text := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, text
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, text
	}
	parseFrontmatterLines(lines[1:end], &fm)
	return fm, strings.Join(lines[end+1:], "\n")
}

func parseFrontmatterLines(lines []string, fm *frontmatter) {
	currentList := ""
	inPermission := false
	inToolsMap := false
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		if indent == 0 {
			if key, val, ok := splitKV(trim); ok && strings.TrimSpace(val) == "" {
				normal := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
				if normal == "hooks" || normal == "mcpservers" || normal == "mcp_servers" {
					var block []string
					for i+1 < len(lines) {
						next := lines[i+1]
						if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " \t")) == 0 {
							break
						}
						i++
						block = append(block, next)
					}
					fm.blocks[normal] = strings.Join(block, "\n")
					currentList = ""
					inPermission = false
					inToolsMap = false
					continue
				}
			}
		}
		if strings.HasPrefix(trim, "-") && currentList != "" {
			item := cleanScalar(strings.TrimSpace(strings.TrimPrefix(trim, "-")))
			if item != "" {
				fm.lists[currentList] = append(fm.lists[currentList], item)
			}
			continue
		}
		if indent > 0 && inPermission {
			key, val, ok := splitKV(trim)
			if ok {
				v := strings.ToLower(cleanScalar(val))
				if v == "allow" || v == "deny" || v == "ask" {
					fm.permission[strings.ToLower(key)] = v
				}
			}
			continue
		}
		if indent > 0 && inToolsMap {
			key, val, ok := splitKV(trim)
			if ok {
				v := strings.ToLower(cleanScalar(val))
				if v == "true" || v == "false" {
					fm.toolFlags[strings.ToLower(strings.TrimSpace(key))] = v == "true"
				}
			}
			continue
		}
		currentList = ""
		inPermission = false
		inToolsMap = false
		key, val, ok := splitKV(trim)
		if !ok {
			continue
		}
		key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		if key == "permission" && strings.TrimSpace(val) == "" {
			inPermission = true
			continue
		}
		if key == "tools" && strings.TrimSpace(val) == "" {
			// Claude commonly uses a YAML list here; legacy OpenCode uses a
			// name:boolean map. Keep both parsers active until the first child
			// line reveals which representation the file uses.
			currentList = key
			inToolsMap = true
			continue
		}
		v := strings.TrimSpace(val)
		if v == "|" || v == ">" || v == "|-" || v == ">-" || v == "|+" || v == ">+" {
			fold := strings.HasPrefix(v, ">")
			var block []string
			for i+1 < len(lines) {
				next := lines[i+1]
				if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " \t")) == 0 {
					break
				}
				i++
				block = append(block, strings.TrimSpace(next))
			}
			if fold {
				fm.scalars[key] = strings.Join(block, " ")
			} else {
				fm.scalars[key] = strings.Join(block, "\n")
			}
			continue
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			fm.lists[key] = parseInlineList(v)
			continue
		}
		if v == "" && isListKey(key) {
			currentList = key
			continue
		}
		if isListKey(key) && strings.Contains(v, ",") {
			fm.lists[key] = splitList(v)
			continue
		}
		fm.scalars[key] = cleanScalar(v)
		if isListKey(key) && v != "" {
			fm.lists[key] = splitList(v)
		}
	}
}

func splitKV(line string) (string, string, bool) {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func isListKey(key string) bool {
	switch key {
	case "tools", "disallowedtools", "disallowed_tools", "skills", "mcpservers", "mcp_servers":
		return true
	default:
		return false
	}
}

func parseInlineList(v string) []string {
	return splitList(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")))
}

func splitList(v string) []string {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' })
	if len(parts) == 1 && !strings.Contains(v, ",") {
		// Claude examples also accept whitespace-separated simple values poorly in
		// hand-written files; preserve one item unless commas are present.
		parts = []string{v}
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := cleanScalar(strings.TrimSpace(part))
		if item != "" {
			out = append(out, item)
		}
	}
	return appendUnique(nil, out...)
}

func listValue(fm frontmatter, key string) []string {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if v := fm.lists[key]; len(v) > 0 {
		return append([]string(nil), v...)
	}
	if v := fm.scalars[key]; v != "" {
		return splitList(v)
	}
	return nil
}

func cleanScalar(v string) string { return strings.Trim(strings.TrimSpace(v), `"'`) }
func parseBool(v string) bool     { b, _ := strconv.ParseBool(strings.ToLower(cleanScalar(v))); return b }
func parseInt(v string) int {
	n, _ := strconv.Atoi(cleanScalar(v))
	if n < 0 {
		return 0
	}
	return n
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func appendUnique(dst []string, values ...string) []string {
	seen := map[string]bool{}
	for _, v := range dst {
		seen[strings.ToLower(v)] = true
	}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		dst = append(dst, v)
	}
	return dst
}

// Find resolves agent names case-insensitively.
func Find(list []Agent, name string) *Agent {
	for i := range list {
		if strings.EqualFold(list[i].Name, strings.TrimSpace(name)) {
			return &list[i]
		}
	}
	return nil
}

// FormatForPrompt exposes only routing metadata to the main model. The full
// agent body stays out of the parent context until the Agent tool launches it.
func FormatForPrompt(list []Agent) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<available_agents>\n")
	visible := 0
	for _, a := range list {
		if a.Hidden {
			continue
		}
		visible++
		fmt.Fprintf(&b, "- %s: %s\n", a.Name, strings.ReplaceAll(strings.TrimSpace(a.Description), "\n", " "))
	}
	if visible == 0 {
		return ""
	}
	b.WriteString("</available_agents>\n")
	b.WriteString("Use the Agent tool proactively for self-contained work that benefits from isolated context, especially exploration, research, review, or parallelizable specialist tasks. Delegate a complete task with enough context; the parent receives the subagent's result. For independent work, issue multiple Agent calls in the same response so they can run concurrently, then orchestrate their results. Subagents may delegate further while below the nesting limit. Do not delegate trivial work or work that depends heavily on unstated conversation details.\n")
	return b.String()
}
