package tools

import (
	"context"
	"fmt"
	"strings"

	limemory "github.com/lilith/li/internal/memory"
)

func init() {
	register(Definition{
		Name:          "memory_read",
		Description:   "Read a file from the current agent's persistent memory directory. Paths are relative to that directory; MEMORY.md is the curated entry point.",
		PromptSnippet: "Read persistent agent memory",
		Parameters:    map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Relative path inside the memory directory. Defaults to MEMORY.md."}}},
		Available:     func(env Env) bool { return strings.TrimSpace(env.MemoryDir) != "" },
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			path := strings.TrimSpace(str(args, "path"))
			if path == "" {
				path = "MEMORY.md"
			}
			out, err := limemory.ReadFile(env.MemoryDir, path)
			if err != nil {
				return "", err
			}
			return out, nil
		},
	})
	register(Definition{
		Name:          "memory_write",
		Description:   "Create or replace a file inside the current agent's persistent memory directory. Keep MEMORY.md concise and curate outdated notes instead of appending forever.",
		PromptSnippet: "Update persistent agent memory",
		Mutating:      true,
		Parameters:    map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Relative path inside memory. Defaults to MEMORY.md."}, "content": map[string]any{"type": "string"}}, "required": []string{"content"}},
		Available:     func(env Env) bool { return strings.TrimSpace(env.MemoryDir) != "" },
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			path := strings.TrimSpace(str(args, "path"))
			if path == "" {
				path = "MEMORY.md"
			}
			content := str(args, "content")
			if err := limemory.WriteFile(env.MemoryDir, path, content); err != nil {
				return "", err
			}
			return fmt.Sprintf("memory updated: %s", path), nil
		},
	})
}
