// Package hooks implements the portable command, HTTP and MCP-tool subset of
// Claude Code lifecycle hooks. Project hooks are only loaded for trusted
// workspaces; user hooks may run globally. Hook commands receive
// Claude-compatible JSON on stdin.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/lilith/li/internal/toolchain"
)

type Hook struct {
	Type           string            `json:"type"`
	Command        string            `json:"command,omitempty"`
	URL            string            `json:"url,omitempty"`
	Server         string            `json:"server,omitempty"`
	Tool           string            `json:"tool,omitempty"`
	Input          map[string]any    `json:"input,omitempty"`
	Timeout        float64           `json:"timeout,omitempty"`
	Async          bool              `json:"async,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`

	PluginRoot string `json:"-"`
	PluginData string `json:"-"`
	ProjectDir string `json:"-"`
}

type Matcher struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []Hook `json:"hooks"`
}

type Settings struct {
	Hooks map[string][]Matcher `json:"hooks"`
}

type Runner struct {
	Root    string
	Entries map[string][]Matcher
	MCPTool func(context.Context, string, string, map[string]any) (string, error)
}

type Result struct {
	Blocked           bool
	Reason            string
	AdditionalContext string
	UpdatedInput      map[string]any
	UpdatedOutput     string
	SystemMessage     string
	WorktreePath      string
}

func Load(configDir, root string, includeProject bool) *Runner {
	r := &Runner{Root: root, Entries: map[string][]Matcher{}}
	home := filepath.Dir(filepath.Clean(configDir))
	r.merge(filepath.Join(home, ".claude", "settings.json"))
	if includeProject {
		r.merge(filepath.Join(root, ".claude", "settings.json"))
		r.merge(filepath.Join(root, ".claude", "settings.local.json"))
	}
	return r
}

func (r *Runner) merge(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r.Merge(ParseJSON(data, r.Root))
}

// LoadFile reads one Claude hooks JSON file. It accepts both the documented
// {"hooks": {...}} wrapper and a direct event map, which plugin manifests may
// embed inline.
func LoadFile(path, root string) *Runner {
	data, err := os.ReadFile(path)
	if err != nil {
		return &Runner{Root: root, Entries: map[string][]Matcher{}}
	}
	return ParseJSON(data, root)
}

// ParseJSON parses a hooks configuration without executing it.
func ParseJSON(data []byte, root string) *Runner {
	r := &Runner{Root: root, Entries: map[string][]Matcher{}}
	var wrapped Settings
	if json.Unmarshal(data, &wrapped) == nil && wrapped.Hooks != nil {
		r.Entries = wrapped.Hooks
		return r
	}
	var direct map[string][]Matcher
	if json.Unmarshal(data, &direct) == nil {
		r.Entries = direct
	}
	return r
}

// SetPluginContext marks all handlers in the runner as plugin-owned so path
// placeholders and environment variables resolve to the plugin installation.
func (r *Runner) SetPluginContext(pluginRoot, pluginData, projectDir string) {
	if r == nil {
		return
	}
	for event, groups := range r.Entries {
		for i := range groups {
			for j := range groups[i].Hooks {
				groups[i].Hooks[j].PluginRoot = pluginRoot
				groups[i].Hooks[j].PluginData = pluginData
				groups[i].Hooks[j].ProjectDir = projectDir
			}
		}
		r.Entries[event] = groups
	}
}

// Merge appends another runner's lifecycle entries while preserving order.
func (r *Runner) Merge(other *Runner) {
	if r == nil || other == nil {
		return
	}
	if r.Entries == nil {
		r.Entries = map[string][]Matcher{}
	}
	for event, entries := range other.Entries {
		r.Entries[event] = append(r.Entries[event], entries...)
	}
}

// ParseFrontmatter parses the nested `hooks:` payload used inside Claude
// skill/subagent frontmatter. It intentionally supports the documented hook
// structure (event -> matcher groups -> hook handlers) rather than arbitrary
// YAML. JSON is accepted too, which is convenient for generated definitions.
func ParseFrontmatter(raw, root string) *Runner {
	r := &Runner{Root: root, Entries: map[string][]Matcher{}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return r
	}
	if strings.HasPrefix(raw, "{") {
		var wrapper struct {
			Hooks map[string][]Matcher `json:"hooks"`
		}
		if json.Unmarshal([]byte(raw), &wrapper) == nil && wrapper.Hooks != nil {
			r.Entries = wrapper.Hooks
			return r
		}
		var direct map[string][]Matcher
		if json.Unmarshal([]byte(raw), &direct) == nil {
			r.Entries = direct
			return r
		}
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent < 0 || n < minIndent {
			minIndent = n
		}
	}
	if minIndent > 0 {
		for i, line := range lines {
			if len(line) >= minIndent {
				lines[i] = line[minIndent:]
			}
		}
	}
	// Capture a conservative YAML subset. Every event owns a sequence of matcher
	// groups; each group owns a sequence of hook maps. Scalars may be quoted.
	var event string
	var group *Matcher
	var hook *Hook
	flushHook := func() {
		if group == nil || hook == nil {
			return
		}
		if strings.TrimSpace(hook.Type) == "" {
			hook.Type = "command"
		}
		group.Hooks = append(group.Hooks, *hook)
		hook = nil
	}
	flushGroup := func() {
		flushHook()
		if event != "" && group != nil && len(group.Hooks) > 0 {
			r.Entries[event] = append(r.Entries[event], *group)
		}
		group = nil
	}
	for _, rawLine := range lines {
		if strings.TrimSpace(rawLine) == "" || strings.HasPrefix(strings.TrimSpace(rawLine), "#") {
			continue
		}
		indent := len(rawLine) - len(strings.TrimLeft(rawLine, " \t"))
		trim := strings.TrimSpace(rawLine)
		if indent == 0 && strings.HasSuffix(trim, ":") {
			flushGroup()
			event = strings.TrimSpace(strings.TrimSuffix(trim, ":"))
			continue
		}
		// When captured from frontmatter the common indentation is retained. If
		// every line starts indented, normalize event lines on first sight.
		if event == "" && strings.HasSuffix(trim, ":") && !strings.HasPrefix(trim, "-") {
			flushGroup()
			event = strings.TrimSpace(strings.TrimSuffix(trim, ":"))
			continue
		}
		if event == "" {
			continue
		}
		if strings.HasPrefix(trim, "- matcher:") {
			flushGroup()
			group = &Matcher{Matcher: yamlScalar(strings.TrimSpace(strings.TrimPrefix(trim, "- matcher:")))}
			continue
		}
		if strings.HasPrefix(trim, "matcher:") {
			if group == nil {
				group = &Matcher{}
			}
			group.Matcher = yamlScalar(strings.TrimSpace(strings.TrimPrefix(trim, "matcher:")))
			continue
		}
		if trim == "- hooks:" || trim == "hooks:" {
			continue
		}
		if strings.HasPrefix(trim, "- type:") {
			flushHook()
			if group == nil {
				group = &Matcher{}
			}
			hook = &Hook{Type: yamlScalar(strings.TrimSpace(strings.TrimPrefix(trim, "- type:")))}
			continue
		}
		if strings.HasPrefix(trim, "- command:") {
			flushHook()
			if group == nil {
				group = &Matcher{}
			}
			hook = &Hook{Type: "command", Command: yamlScalar(strings.TrimSpace(strings.TrimPrefix(trim, "- command:")))}
			continue
		}
		if strings.HasPrefix(trim, "-") && strings.TrimSpace(strings.TrimPrefix(trim, "-")) == "" {
			continue
		}
		key, value, ok := yamlKV(trim)
		if !ok {
			continue
		}
		if hook == nil {
			hook = &Hook{}
		}
		switch strings.ToLower(key) {
		case "type":
			hook.Type = yamlScalar(value)
		case "command":
			hook.Command = yamlScalar(value)
		case "url":
			hook.URL = yamlScalar(value)
		case "server":
			hook.Server = yamlScalar(value)
		case "tool":
			hook.Tool = yamlScalar(value)
		case "input":
			var input map[string]any
			if json.Unmarshal([]byte(value), &input) == nil {
				hook.Input = input
			}
		case "timeout":
			fmt.Sscanf(yamlScalar(value), "%f", &hook.Timeout)
		case "async":
			hook.Async = parseYAMLBool(value)
		}
	}
	flushGroup()
	return r
}

func yamlKV(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}
func yamlScalar(v string) string { return strings.Trim(strings.TrimSpace(v), `"'`) }
func parseYAMLBool(v string) bool {
	switch strings.ToLower(yamlScalar(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (r *Runner) Has(event string) bool {
	if r == nil {
		return false
	}
	for _, group := range r.Entries[event] {
		if len(group.Hooks) > 0 {
			return true
		}
	}
	return false
}

func (r *Runner) Count() int {
	n := 0
	for _, e := range r.Entries {
		for _, m := range e {
			n += len(m.Hooks)
		}
	}
	return n
}

func (r *Runner) Run(ctx context.Context, event, matcher string, input map[string]any) (Result, error) {
	var combined Result
	entries := r.Entries[event]
	for _, group := range entries {
		if !matches(group.Matcher, matcher) {
			continue
		}
		for _, h := range group.Hooks {
			if h.Async && strings.EqualFold(h.Type, "command") {
				go func(h Hook) { _, _ = r.runOne(context.Background(), event, input, h) }(h)
				continue
			}
			res, err := r.runOne(ctx, event, input, h)
			if err != nil {
				return combined, err
			}
			if res.AdditionalContext != "" {
				combined.AdditionalContext = strings.TrimSpace(combined.AdditionalContext + "\n" + res.AdditionalContext)
			}
			if res.SystemMessage != "" {
				combined.SystemMessage = strings.TrimSpace(combined.SystemMessage + "\n" + res.SystemMessage)
			}
			if res.UpdatedInput != nil {
				combined.UpdatedInput = res.UpdatedInput
			}
			if res.UpdatedOutput != "" {
				combined.UpdatedOutput = res.UpdatedOutput
			}
			if res.WorktreePath != "" {
				combined.WorktreePath = res.WorktreePath
			}
			if res.Blocked {
				combined.Blocked = true
				combined.Reason = res.Reason
				return combined, nil
			}
		}
	}
	return combined, nil
}

func (r *Runner) runOne(parent context.Context, event string, input map[string]any, h Hook) (Result, error) {
	if h.ProjectDir == "" {
		h.ProjectDir = r.Root
	}
	timeout := time.Duration(h.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	payload := cloneMap(input)
	payload["hook_event_name"] = event
	if _, ok := payload["cwd"]; !ok {
		payload["cwd"] = r.Root
	}
	data, _ := json.Marshal(payload)
	var body []byte
	var code int
	switch strings.ToLower(strings.TrimSpace(h.Type)) {
	case "command":
		command := expandPluginValue(h.Command, h)
		if strings.TrimSpace(command) == "" {
			return Result{}, nil
		}
		if h.PluginData != "" && strings.Contains(h.Command, "${CLAUDE_PLUGIN_DATA}") {
			if err := os.MkdirAll(h.PluginData, 0o700); err != nil {
				return Result{}, err
			}
		}
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
		} else {
			shellPath, prefix, ok := toolchain.ShellCommand()
			if !ok {
				return Result{}, fmt.Errorf("no se encontró bash/sh para ejecutar hook")
			}
			args := append(append([]string(nil), prefix...), command)
			cmd = exec.CommandContext(ctx, shellPath, args...)
		}
		cmd.Dir = r.Root
		cmd.Stdin = bytes.NewReader(data)
		projectDir := r.Root
		if h.ProjectDir != "" {
			projectDir = h.ProjectDir
		}
		cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+projectDir, "LILITH_PROJECT_DIR="+projectDir)
		if h.PluginRoot != "" {
			cmd.Env = append(cmd.Env, "CLAUDE_PLUGIN_ROOT="+h.PluginRoot)
		}
		if h.PluginData != "" {
			cmd.Env = append(cmd.Env, "CLAUDE_PLUGIN_DATA="+h.PluginData)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		body = stdout.Bytes()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				return Result{}, err
			}
		}
		if code == 2 {
			reason := strings.TrimSpace(stderr.String())
			if reason == "" {
				reason = "blocked by hook"
			}
			return Result{Blocked: true, Reason: reason}, nil
		}
		if code != 0 {
			return Result{}, fmt.Errorf("hook command failed (%d): %s", code, strings.TrimSpace(stderr.String()))
		}
	case "http":
		url := expandEnv(expandPluginValue(h.URL, h), h.AllowedEnvVars)
		if strings.TrimSpace(url) == "" {
			return Result{}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range h.Headers {
			req.Header.Set(k, expandEnv(expandPluginValue(v, h), h.AllowedEnvVars))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return Result{}, err
		}
		defer resp.Body.Close()
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Result{}, fmt.Errorf("hook http %s", resp.Status)
		}
	case "mcp_tool":
		server := strings.TrimSpace(expandPluginValue(h.Server, h))
		tool := strings.TrimSpace(expandPluginValue(h.Tool, h))
		if server == "" || tool == "" {
			return Result{SystemMessage: "hook mcp_tool omitido: faltan server o tool"}, nil
		}
		if r.MCPTool == nil {
			return Result{SystemMessage: fmt.Sprintf("hook mcp_tool %s/%s no disponible: MCP aún no está conectado", server, tool)}, nil
		}
		resolved := resolveInput(h.Input, payload)
		text, err := r.MCPTool(ctx, server, tool, resolved)
		if err != nil {
			// Claude treats MCP-hook transport failures as non-blocking hook
			// failures. Surface the issue to the model/UI without preventing the
			// lifecycle event that triggered it.
			return Result{SystemMessage: fmt.Sprintf("hook mcp_tool %s/%s falló: %v", server, tool, err)}, nil
		}
		body = []byte(text)
	default:
		return Result{}, nil
	}
	if event == "WorktreeCreate" {
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" && !strings.HasPrefix(trimmed, "{") {
			return Result{WorktreePath: trimmed}, nil
		}
	}
	return parseOutput(body), nil
}

func expandPluginValue(value string, h Hook) string {
	projectDir := h.ProjectDir
	value = strings.ReplaceAll(value, "${CLAUDE_PLUGIN_ROOT}", h.PluginRoot)
	value = strings.ReplaceAll(value, "${CLAUDE_PLUGIN_DATA}", h.PluginData)
	value = strings.ReplaceAll(value, "${CLAUDE_PROJECT_DIR}", projectDir)
	return value
}

var templatePattern = regexp.MustCompile(`\$\{([^{}]+)\}`)

func resolveInput(input map[string]any, payload map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	resolved, _ := resolveValue(input, payload).(map[string]any)
	if resolved == nil {
		return map[string]any{}
	}
	return resolved
}

func resolveValue(value any, payload map[string]any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = resolveValue(item, payload)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = resolveValue(item, payload)
		}
		return out
	case string:
		matches := templatePattern.FindAllStringSubmatchIndex(typed, -1)
		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(typed) {
			if found, ok := lookupPath(payload, typed[matches[0][2]:matches[0][3]]); ok {
				return cloneValue(found)
			}
		}
		return templatePattern.ReplaceAllStringFunc(typed, func(match string) string {
			parts := templatePattern.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			found, ok := lookupPath(payload, parts[1])
			if !ok {
				return match
			}
			return fmt.Sprint(found)
		})
	default:
		return typed
	}
}

func lookupPath(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		if part == "" {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return typed
	}
}

func parseOutput(data []byte) Result {
	if len(bytes.TrimSpace(data)) == 0 {
		return Result{}
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return Result{}
	}
	r := Result{}
	if d, _ := raw["decision"].(string); strings.EqualFold(d, "block") {
		r.Blocked = true
		r.Reason = strValue(raw["reason"])
	}
	r.SystemMessage = strValue(raw["systemMessage"])
	if hs, ok := raw["hookSpecificOutput"].(map[string]any); ok {
		if d := strings.ToLower(strValue(hs["permissionDecision"])); d == "deny" {
			r.Blocked = true
			r.Reason = strValue(hs["permissionDecisionReason"])
		}
		r.AdditionalContext = strValue(hs["additionalContext"])
		r.UpdatedOutput = strValue(hs["updatedToolOutput"])
		r.WorktreePath = strValue(hs["worktreePath"])
		if u, ok := hs["updatedInput"].(map[string]any); ok {
			r.UpdatedInput = u
		}
	}
	if r.AdditionalContext == "" {
		r.AdditionalContext = strValue(raw["additionalContext"])
	}
	return r
}

func matches(pattern, value string) bool {
	p := strings.TrimSpace(pattern)
	if p == "" || p == "*" {
		return true
	}
	if p == value || strings.EqualFold(p, value) {
		return true
	}
	re, err := regexp.Compile(p)
	return err == nil && re.MatchString(value)
}
func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func strValue(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func expandEnv(s string, allow []string) string {
	for _, k := range allow {
		s = strings.ReplaceAll(s, "$"+k, os.Getenv(k))
		s = strings.ReplaceAll(s, "${"+k+"}", os.Getenv(k))
	}
	return s
}
