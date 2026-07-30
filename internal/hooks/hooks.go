// Package hooks implements the command/HTTP subset of Claude Code lifecycle
// hooks. Project hooks are only loaded for trusted workspaces; user hooks may
// run globally. Hook commands receive Claude-compatible JSON on stdin.
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
)

type Hook struct {
	Type           string            `json:"type"`
	Command        string            `json:"command,omitempty"`
	URL            string            `json:"url,omitempty"`
	Timeout        float64           `json:"timeout,omitempty"`
	Async          bool              `json:"async,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`
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
	var s Settings
	if json.Unmarshal(data, &s) != nil {
		return
	}
	for event, entries := range s.Hooks {
		r.Entries[event] = append(r.Entries[event], entries...)
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
		if strings.TrimSpace(h.Command) == "" {
			return Result{}, nil
		}
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd.exe", "/C", h.Command)
		} else {
			cmd = exec.CommandContext(ctx, "/bin/sh", "-c", h.Command)
		}
		cmd.Dir = r.Root
		cmd.Stdin = bytes.NewReader(data)
		cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+r.Root, "LILITH_PROJECT_DIR="+r.Root)
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
		if strings.TrimSpace(h.URL) == "" {
			return Result{}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, expandEnv(h.URL, h.AllowedEnvVars), bytes.NewReader(data))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range h.Headers {
			req.Header.Set(k, expandEnv(v, h.AllowedEnvVars))
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
