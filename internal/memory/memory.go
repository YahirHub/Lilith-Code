// Package memory implements Claude-compatible persistent subagent memory plus
// a Lilith-native project memory file. Memory is deliberately file-based so a
// project-scoped agent memory can be shared through version control.
package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	MaxStartupLines = 200
	MaxStartupBytes = 25 * 1024
)

type Scope string

const (
	User    Scope = "user"
	Project Scope = "project"
	Local   Scope = "local"
)

// AgentDir returns the Claude-compatible persistent directory for an agent.
func AgentDir(configDir, projectRoot, agentName string, scope Scope) string {
	name := sanitizeName(agentName)
	switch scope {
	case User:
		home := filepath.Dir(filepath.Clean(configDir))
		return filepath.Join(home, ".claude", "agent-memory", name)
	case Project:
		return filepath.Join(projectRoot, ".claude", "agent-memory", name)
	case Local:
		return filepath.Join(projectRoot, ".claude", "agent-memory-local", name)
	default:
		return ""
	}
}

// ProjectDir is Lilith's native main-conversation auto-memory directory.
func ProjectDir(configDir, projectRoot string) string {
	return filepath.Join(configDir, "memory", projectKey(projectRoot))
}

// ClaudeProjectDir returns Claude Code's auto-memory directory for the current
// repository. Worktrees are normalized to the main checkout so Claude and
// Lilith share MEMORY.md across all worktrees of the same repository. Project
// settings are executable workspace configuration and therefore only override
// the path after the caller has marked the project trusted.
func ClaudeProjectDir(configDir, projectRoot string, trusted bool) string {
	if strings.TrimSpace(os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY")) == "1" {
		return ""
	}
	if custom := claudeAutoMemoryDirectory(configDir, projectRoot, trusted); custom != "" {
		return custom
	}
	home := filepath.Dir(filepath.Clean(configDir))
	if home == "." || home == string(filepath.Separator) || strings.TrimSpace(configDir) == "" {
		home, _ = os.UserHomeDir()
	}
	root := canonicalRepoRoot(projectRoot)
	if root == "" {
		root, _ = filepath.Abs(projectRoot)
	}
	return filepath.Join(home, ".claude", "projects", claudeProjectSlug(root), "memory")
}

func claudeAutoMemoryDirectory(configDir, projectRoot string, trusted bool) string {
	home := filepath.Dir(filepath.Clean(configDir))
	if strings.TrimSpace(configDir) == "" {
		home, _ = os.UserHomeDir()
	}
	candidates := []string{}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".claude", "settings.json"))
	}
	if trusted && strings.TrimSpace(projectRoot) != "" {
		candidates = append(candidates, filepath.Join(projectRoot, ".claude", "settings.json"), filepath.Join(projectRoot, ".claude", "settings.local.json"))
	}
	value := ""
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			AutoMemoryDirectory string `json:"autoMemoryDirectory"`
		}
		if json.Unmarshal(data, &raw) == nil && strings.TrimSpace(raw.AutoMemoryDirectory) != "" {
			value = strings.TrimSpace(raw.AutoMemoryDirectory)
		}
	}
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		return filepath.Join(home, filepath.FromSlash(strings.TrimLeft(value[2:], `/\`)))
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	// Claude accepts only absolute or ~/ paths. Invalid project settings fail
	// closed instead of silently writing memory relative to the repository.
	return ""
}

func canonicalRepoRoot(projectRoot string) string {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return ""
	}
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				p := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
				if p != "" {
					if abs, e := filepath.Abs(p); e == nil {
						return filepath.Clean(abs)
					}
					return filepath.Clean(p)
				}
			}
		}
	}
	cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		return filepath.Clean(strings.TrimSpace(string(out)))
	}
	abs, _ := filepath.Abs(root)
	return filepath.Clean(abs)
}

func claudeProjectSlug(root string) string {
	root = filepath.Clean(root)
	var b strings.Builder
	for _, r := range root {
		switch r {
		case '/', '\\', '.', ':':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func MemoryPath(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "MEMORY.md")
}

func Ensure(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("memory directory is empty")
	}
	return os.MkdirAll(dir, 0o700)
}

// ReadStartup reads at most Claude's startup budget: first 200 lines or 25KB.
func ReadStartup(dir string) (string, error) {
	path := MemoryPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if len(data) > MaxStartupBytes {
		data = data[:MaxStartupBytes]
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > MaxStartupLines {
		lines = lines[:MaxStartupLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func ReadFile(dir, relative string) (string, error) {
	path, err := resolve(dir, relative)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteFile(dir, relative, content string) error {
	if err := Ensure(dir); err != nil {
		return err
	}
	path, err := resolve(dir, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func Prompt(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	_ = Ensure(dir)
	startup, _ := ReadStartup(dir)
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nPersistent agent memory is enabled. Your memory directory is %s. Use memory_read and memory_write to maintain concise reusable knowledge. Curate MEMORY.md rather than appending unbounded logs.", filepath.ToSlash(dir))
	if startup != "" {
		b.WriteString("\n\n<agent_memory>\n")
		b.WriteString(startup)
		b.WriteString("\n</agent_memory>")
	}
	return b.String()
}

func resolve(dir, relative string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("memory unavailable")
	}
	rel := strings.TrimSpace(relative)
	if rel == "" {
		rel = "MEMORY.md"
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("memory path must be relative")
	}
	base, _ := filepath.Abs(dir)
	target, _ := filepath.Abs(filepath.Join(base, filepath.Clean(rel)))
	prefix := base + string(filepath.Separator)
	if target != base && !strings.HasPrefix(target, prefix) {
		return "", errors.New("memory path escapes memory directory")
	}
	return target, nil
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "agent"
	}
	var out []rune
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func projectKey(project string) string {
	// Human-readable and stable enough for auto-memory; sessions use a hash,
	// but memory doesn't need to share that private helper.
	clean := filepath.Clean(project)
	base := sanitizeName(filepath.Base(clean))
	if base == "" || base == "." {
		base = "project"
	}
	// Avoid importing crypto solely for a path: encode normalized absolute path
	// as a deterministic FNV-1a suffix.
	var h uint64 = 1469598103934665603
	for _, c := range []byte(strings.ToLower(filepath.ToSlash(clean))) {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return fmt.Sprintf("%s-%08x", base, uint32(h^(h>>32)))
}
