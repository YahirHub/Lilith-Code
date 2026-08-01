package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigAcceptsWrappedAndInlineMaps(t *testing.T) {
	wrapped := ParseConfig([]byte(`{"mcpServers":{"db":{"command":"server"}}}`))
	if wrapped["db"].Type != "stdio" || wrapped["db"].Name != "db" {
		t.Fatalf("wrapped=%#v", wrapped)
	}
	inline := ParseConfig([]byte(`{"api":{"type":"sse","url":"https://example.invalid/sse"}}`))
	if inline["api"].Type != "sse" || inline["api"].Name != "api" {
		t.Fatalf("inline=%#v", inline)
	}
}

func TestScopePluginConfigsNamespacesAndExpandsPaths(t *testing.T) {
	root := filepath.Join("tmp", "plugin")
	data := filepath.Join("tmp", "data")
	project := filepath.Join("tmp", "project")
	got := ScopePluginConfigs("quality-tools", root, data, project, map[string]ServerConfig{
		"db-main": {
			Command: "${CLAUDE_PLUGIN_ROOT}/bin/server",
			Args:    []string{"--data", "${CLAUDE_PLUGIN_DATA}", "--project", "${CLAUDE_PROJECT_DIR}"},
			Env:     map[string]string{"DB": "${CLAUDE_PLUGIN_DATA}/db"},
			Headers: map[string]string{"X-Root": "${CLAUDE_PLUGIN_ROOT}"},
		},
	})
	cfg, ok := got["plugin_quality-tools_db-main"]
	if !ok {
		t.Fatalf("scoped configs=%#v", got)
	}
	if cfg.Type != "stdio" || filepath.Clean(cfg.Command) != filepath.Clean(filepath.Join(root, "bin", "server")) {
		t.Fatalf("config=%#v", cfg)
	}
	joined := strings.Join(cfg.Args, " ")
	if !strings.Contains(joined, data) || !strings.Contains(joined, project) {
		t.Fatalf("args=%#v", cfg.Args)
	}
	if cfg.Env["CLAUDE_PLUGIN_ROOT"] != root || cfg.Env["CLAUDE_PLUGIN_DATA"] != data || cfg.Env["CLAUDE_PROJECT_DIR"] != project {
		t.Fatalf("env=%#v", cfg.Env)
	}
	if cfg.Headers["X-Root"] != root {
		t.Fatalf("headers=%#v", cfg.Headers)
	}
}
