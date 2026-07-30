// Package instructions loads Lilith-native project instructions and the
// portable Claude Code instruction hierarchy (CLAUDE.md + .claude/rules).
package instructions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Options struct {
	ConfigDir      string
	CWD            string
	NativeEnabled  bool
	ClaudeEnabled  bool
	ProjectTrusted bool
}

type Document struct {
	Path    string
	Kind    string // lilith | claude | claude-local | rule | memory
	Scope   string // user | project
	Content string
}

type Rule struct {
	Document Document
	Paths    []string
}

type Bundle struct {
	Always      []Document
	Conditional []Rule
	cwd         string
	claude      bool
	excludes    []string
	root        string
	trusted     bool
}

func Load(opts Options) Bundle {
	cwd := cleanAbs(opts.CWD)
	if cwd == "" {
		cwd, _ = os.Getwd()
		cwd = cleanAbs(cwd)
	}
	root := findRepoRoot(cwd)
	excludes := []string{}
	if opts.ClaudeEnabled {
		excludes = claudeMdExcludes(opts.ConfigDir, root)
	}
	b := Bundle{cwd: cwd, claude: opts.ClaudeEnabled, excludes: append([]string(nil), excludes...), root: root, trusted: opts.ProjectTrusted}
	seen := map[string]bool{}
	appendDoc := func(path, kind, scope string) {
		path = cleanAbs(path)
		if path == "" || seen[path] || excluded(path, excludes) {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		content, err := readExpanded(path, 0, map[string]bool{}, importPolicy{Root: root, AllowExternal: scope == "user" || opts.ProjectTrusted})
		if err != nil || strings.TrimSpace(content) == "" {
			return
		}
		seen[path] = true
		b.Always = append(b.Always, Document{Path: path, Kind: kind, Scope: scope, Content: content})
	}

	home := ""
	if opts.ConfigDir != "" {
		home = filepath.Dir(filepath.Clean(opts.ConfigDir))
	}
	if opts.NativeEnabled {
		if opts.ConfigDir != "" {
			for _, name := range []string{"LILITH.md", "lilith.md", "LI.md", "li.md"} {
				appendDoc(filepath.Join(opts.ConfigDir, name), "lilith", "user")
			}
		}
		for _, dir := range hierarchy(root, cwd) {
			for _, name := range []string{"LILITH.md", "lilith.md", "LI.md", "li.md"} {
				appendDoc(filepath.Join(dir, name), "lilith", "project")
			}
			appendDoc(filepath.Join(dir, ".li", "LILITH.md"), "lilith", "project")
			appendDoc(filepath.Join(dir, ".li", "LI.md"), "lilith", "project")
		}
	}

	if opts.ClaudeEnabled {
		if home != "" {
			appendDoc(filepath.Join(home, ".claude", "CLAUDE.md"), "claude", "user")
			loadRules(filepath.Join(home, ".claude", "rules"), "user", excludes, seen, &b, root, true)
		}
		for _, dir := range hierarchy(root, cwd) {
			appendDoc(filepath.Join(dir, "CLAUDE.md"), "claude", "project")
			appendDoc(filepath.Join(dir, ".claude", "CLAUDE.md"), "claude", "project")
			appendDoc(filepath.Join(dir, "CLAUDE.local.md"), "claude-local", "project")
			loadRules(filepath.Join(dir, ".claude", "rules"), "project", excludes, seen, &b, root, opts.ProjectTrusted)
		}
	}
	return b
}

func (b Bundle) StaticPrompt() string {
	if len(b.Always) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<project_instructions>\n")
	out.WriteString("Treat these files as persistent project/user guidance. More specific files loaded later may refine earlier guidance.\n")
	for _, d := range b.Always {
		fmt.Fprintf(&out, "\n<instructions source=%q kind=%q>\n%s\n</instructions>\n", filepath.ToSlash(d.Path), d.Kind, strings.TrimSpace(d.Content))
	}
	out.WriteString("</project_instructions>")
	return out.String()
}

func (b Bundle) ConditionalPrompt(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var docs []Document
	seen := map[string]bool{}
	for _, d := range b.Always {
		seen[cleanAbs(d.Path)] = true
	}
	for _, rule := range b.Conditional {
		if !ruleMatches(rule, paths) || seen[cleanAbs(rule.Document.Path)] {
			continue
		}
		seen[cleanAbs(rule.Document.Path)] = true
		docs = append(docs, rule.Document)
	}
	// Claude loads CLAUDE.md/CLAUDE.local.md below the launch directory only
	// when files in those subdirectories are read. Reproduce that lazy behavior
	// so large monorepos do not pay for unrelated team instructions.
	if b.claude {
		for _, d := range nestedClaudeDocuments(b.cwd, paths, b.excludes, seen, b.projectRoot(), b.projectTrusted()) {
			seen[cleanAbs(d.Path)] = true
			docs = append(docs, d)
		}
	}
	if len(docs) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<path_scoped_instructions>\n")
	for _, d := range docs {
		tag := "rule"
		if d.Kind == "claude" || d.Kind == "claude-local" {
			tag = "instructions"
		}
		fmt.Fprintf(&out, "<%s source=%q>\n%s\n</%s>\n", tag, filepath.ToSlash(d.Path), strings.TrimSpace(d.Content), tag)
	}
	out.WriteString("</path_scoped_instructions>")
	return out.String()
}

func nestedClaudeDocuments(cwd string, paths, excludes []string, seen map[string]bool, root string, trusted bool) []Document {
	cwd = cleanAbs(cwd)
	if cwd == "" {
		return nil
	}
	dirSeen := map[string]bool{}
	var dirs []string
	for _, raw := range paths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, filepath.FromSlash(p))
		}
		p = cleanAbs(p)
		rel, err := filepath.Rel(cwd, p)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		dir := p
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			dir = filepath.Dir(p)
		} else if filepath.Ext(p) != "" {
			dir = filepath.Dir(p)
		}
		for cur := dir; cur != cwd; cur = filepath.Dir(cur) {
			if cur == "." || cur == string(filepath.Separator) || !within(cwd, cur) {
				break
			}
			if !dirSeen[cur] {
				dirSeen[cur] = true
				dirs = append(dirs, cur)
			}
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		ri, _ := filepath.Rel(cwd, dirs[i])
		rj, _ := filepath.Rel(cwd, dirs[j])
		return strings.Count(filepath.ToSlash(ri), "/") < strings.Count(filepath.ToSlash(rj), "/") || (strings.Count(filepath.ToSlash(ri), "/") == strings.Count(filepath.ToSlash(rj), "/") && ri < rj)
	})
	var out []Document
	for _, dir := range dirs {
		for _, spec := range []struct{ name, kind string }{{"CLAUDE.md", "claude"}, {"CLAUDE.local.md", "claude-local"}, {filepath.Join(".claude", "CLAUDE.md"), "claude"}} {
			path := cleanAbs(filepath.Join(dir, spec.name))
			if path == "" || seen[path] || excluded(path, excludes) {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			content, err := readExpanded(path, 0, map[string]bool{}, importPolicy{Root: root, AllowExternal: trusted})
			if err == nil && strings.TrimSpace(content) != "" {
				out = append(out, Document{Path: path, Kind: spec.kind, Scope: "project", Content: content})
			}
		}
	}
	return out
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (b Bundle) projectRoot() string  { return b.root }
func (b Bundle) projectTrusted() bool { return b.trusted }

func (b Bundle) Sources() []Document {
	out := append([]Document(nil), b.Always...)
	for _, r := range b.Conditional {
		out = append(out, r.Document)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func loadRules(dir, scope string, excludes []string, seen map[string]bool, b *Bundle, root string, trusted bool) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		abs := cleanAbs(path)
		if abs == "" || seen[abs] || excluded(abs, excludes) {
			return nil
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return nil
		}
		paths, body := parseRule(string(data))
		expanded, expErr := expandText(body, filepath.Dir(abs), 0, map[string]bool{abs: true}, importPolicy{Root: root, AllowExternal: scope == "user" || trusted})
		if expErr != nil || strings.TrimSpace(expanded) == "" {
			return nil
		}
		doc := Document{Path: abs, Kind: "rule", Scope: scope, Content: stripHTMLComments(expanded)}
		seen[abs] = true
		if len(paths) == 0 {
			b.Always = append(b.Always, doc)
		} else {
			b.Conditional = append(b.Conditional, Rule{Document: doc, Paths: paths})
		}
		return nil
	})
}

func parseRule(text string) ([]string, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, text
	}
	end := -1
	var paths []string
	inPaths := false
	for i := 1; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if trim == "---" {
			end = i
			break
		}
		if strings.HasPrefix(strings.ToLower(trim), "paths:") {
			inPaths = true
			inline := strings.TrimSpace(trim[len("paths:"):])
			if strings.HasPrefix(inline, "[") && strings.HasSuffix(inline, "]") {
				for _, v := range strings.Split(strings.Trim(inline, "[]"), ",") {
					v = strings.Trim(strings.TrimSpace(v), `"'`)
					if v != "" {
						paths = append(paths, v)
					}
				}
			}
			continue
		}
		if inPaths && strings.HasPrefix(trim, "-") {
			v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "-")), `"'`)
			if v != "" {
				paths = append(paths, v)
			}
			continue
		}
		if len(lines[i])-len(strings.TrimLeft(lines[i], " \t")) == 0 {
			inPaths = false
		}
	}
	if end < 0 {
		return nil, text
	}
	return paths, strings.Join(lines[end+1:], "\n")
}

type importPolicy struct {
	Root          string
	AllowExternal bool
}

func readExpanded(path string, depth int, stack map[string]bool, policy importPolicy) (string, error) {
	abs := cleanAbs(path)
	if abs == "" {
		return "", os.ErrNotExist
	}
	if depth > 5 {
		return "", fmt.Errorf("instruction import depth exceeded at %s", abs)
	}
	if stack[abs] {
		return "", fmt.Errorf("instruction import cycle at %s", abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	next := make(map[string]bool, len(stack)+1)
	for k, v := range stack {
		next[k] = v
	}
	next[abs] = true
	return expandText(string(data), filepath.Dir(abs), depth, next, policy)
}

var importToken = regexp.MustCompile(`@([^\s` + "`" + `()<>]+)`)

func expandText(text, base string, depth int, stack map[string]bool, policy importPolicy) (string, error) {
	text = stripHTMLComments(text)
	lines := strings.Split(text, "\n")
	inFence := false
	changed := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		expanded, didChange := expandLineImports(line, base, depth, stack, policy)
		if didChange {
			lines[i] = expanded
			changed = true
		}
	}
	if !changed {
		return text, nil
	}
	return strings.Join(lines, "\n"), nil
}

func expandLineImports(line, base string, depth int, stack map[string]bool, policy importPolicy) (string, bool) {
	matches := importToken.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line, false
	}
	var out strings.Builder
	last := 0
	changed := false
	for _, m := range matches {
		if insideMarkdownCodeSpan(line, m[0]) {
			continue
		}
		raw := line[m[2]:m[3]]
		candidate := raw
		if strings.HasPrefix(candidate, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				candidate = filepath.Join(home, candidate[2:])
			}
		} else if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, filepath.FromSlash(candidate))
		}
		candidate = cleanAbs(candidate)
		if !policy.AllowExternal && policy.Root != "" && !within(cleanAbs(policy.Root), candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		expanded, err := readExpanded(candidate, depth+1, stack, policy)
		if err != nil {
			continue
		}
		out.WriteString(line[last:m[0]])
		out.WriteString("\n<!-- imported: " + filepath.ToSlash(candidate) + " -->\n")
		out.WriteString(expanded)
		out.WriteString("\n<!-- end import -->")
		last = m[1]
		changed = true
	}
	if !changed {
		return line, false
	}
	out.WriteString(line[last:])
	return out.String(), true
}

func insideMarkdownCodeSpan(line string, pos int) bool {
	if pos <= 0 {
		return false
	}
	// Claude ignores @imports inside Markdown code spans. A lightweight parity
	// check is sufficient for normal CLAUDE.md usage and deliberately treats
	// escaped backticks as literals.
	open := false
	for i := 0; i < pos && i < len(line); i++ {
		if line[i] != '`' || (i > 0 && line[i-1] == '\\') {
			continue
		}
		open = !open
	}
	return open
}

func stripHTMLComments(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inFence := false
	inComment := false
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		work := line
		for {
			if inComment {
				if idx := strings.Index(work, "-->"); idx >= 0 {
					work = work[idx+3:]
					inComment = false
					continue
				}
				work = ""
				break
			}
			start := strings.Index(work, "<!--")
			if start < 0 {
				break
			}
			end := strings.Index(work[start+4:], "-->")
			if end >= 0 {
				work = work[:start] + work[start+4+end+3:]
				continue
			}
			work = work[:start]
			inComment = true
			break
		}
		if strings.TrimSpace(work) != "" {
			out = append(out, work)
		}
	}
	return strings.Join(out, "\n")
}

func claudeMdExcludes(configDir, root string) []string {
	var paths []string
	home := ""
	if configDir != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	candidates := []string{}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".claude", "settings.json"))
	}
	if root != "" {
		candidates = append(candidates, filepath.Join(root, ".claude", "settings.json"), filepath.Join(root, ".claude", "settings.local.json"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var v struct {
			ClaudeMdExcludes []string `json:"claudeMdExcludes"`
		}
		if json.Unmarshal(data, &v) == nil {
			paths = append(paths, v.ClaudeMdExcludes...)
		}
	}
	return paths
}

func excluded(path string, patterns []string) bool {
	path = filepath.ToSlash(cleanAbs(path))
	for _, p := range patterns {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if filepath.IsAbs(filepath.FromSlash(p)) {
			if globMatch(p, path) {
				return true
			}
		} else if globMatch(p, path) || globMatch("**/"+strings.TrimPrefix(p, "./"), path) {
			return true
		}
	}
	return false
}

func ruleMatches(r Rule, paths []string) bool {
	for _, actual := range paths {
		actual = filepath.ToSlash(actual)
		for _, pattern := range r.Paths {
			if globMatch(pattern, actual) || globMatch(pattern, strings.TrimPrefix(actual, "./")) {
				return true
			}
		}
	}
	return false
}

func globMatch(pattern, value string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	value = filepath.ToSlash(value)
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
			continue
		}
		if c == '?' {
			b.WriteString("[^/]")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('$')
	ok, _ := regexp.MatchString(b.String(), value)
	return ok
}

func hierarchy(root, cwd string) []string {
	root, cwd = cleanAbs(root), cleanAbs(cwd)
	if cwd == "" {
		return nil
	}
	var rev []string
	cur := cwd
	for {
		rev = append(rev, cur)
		if cur == root || filepath.Dir(cur) == cur {
			break
		}
		cur = filepath.Dir(cur)
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func findRepoRoot(cwd string) string {
	cur := cleanAbs(cwd)
	for cur != "" {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return cleanAbs(cwd)
}

func cleanAbs(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(a)
}
