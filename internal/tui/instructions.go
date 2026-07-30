package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/instructions"
	limemory "github.com/lilith/li/internal/memory"
	"github.com/lilith/li/internal/providers/openai"
)

func (m *ChatModel) instructionSettings() config.Settings {
	s, _ := config.Load(m.ctx.ConfigDir)
	return s
}

func (m *ChatModel) instructionBundle() instructions.Bundle {
	s := m.instructionSettings()
	return instructions.Load(instructions.Options{
		ConfigDir: m.ctx.ConfigDir, CWD: m.project,
		NativeEnabled:  s.ProjectInstructionsEnabled,
		ClaudeEnabled:  s.ClaudeCompatibilityEnabled,
		ProjectTrusted: config.IsProjectTrusted(s, m.project),
	})
}

func (m *ChatModel) claudeCompatibilityEnabled() bool {
	return m.instructionSettings().ClaudeCompatibilityEnabled
}

func instructionPathsFromHistory(history []openai.Message, root string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if !filepath.IsAbs(v) && root != "" {
			v = filepath.Join(root, filepath.FromSlash(v))
		}
		rel := v
		if root != "" {
			if r, err := filepath.Rel(root, v); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	for _, msg := range history {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			var args any
			if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil {
				collectPathValues(args, "", add)
			}
		}
	}
	return out
}

func collectPathValues(v any, key string, add func(string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			collectPathValues(child, strings.ToLower(k), add)
		}
	case []any:
		for _, child := range x {
			collectPathValues(child, key, add)
		}
	case string:
		switch key {
		case "path", "paths", "file", "files", "file_path", "filepath", "target", "directory", "dir":
			add(x)
		}
	}
}

func (m *ChatModel) mainMemoryDir() string {
	if m == nil || m.ctx == nil {
		return ""
	}
	s := m.instructionSettings()
	if !s.AutoMemoryEnabled {
		return ""
	}
	if s.ClaudeCompatibilityEnabled {
		if dir := limemory.ClaudeProjectDir(m.ctx.ConfigDir, m.project, config.IsProjectTrusted(s, m.project)); dir != "" {
			return dir
		}
	}
	return limemory.ProjectDir(m.ctx.ConfigDir, m.project)
}

func (m *ChatModel) mainMemoryPrompt() string {
	dir := m.mainMemoryDir()
	if dir == "" {
		return ""
	}
	return limemory.Prompt(dir)
}
