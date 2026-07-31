package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/mcp"
	planstate "github.com/lilith/li/internal/plan"
)

type mcpReadyMsg struct {
	runtime   *mcp.Runtime
	signature string
	errors    []error
}

func mcpConfigSignature(configs map[string]mcp.ServerConfig) string {
	if len(configs) == 0 {
		return ""
	}
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]mcp.ServerConfig, 0, len(names))
	for _, name := range names {
		cfg := configs[name]
		cfg.Name = name
		ordered = append(ordered, cfg)
	}
	b, _ := json.Marshal(ordered)
	return string(b)
}

// connectMCP refreshes Claude-compatible MCP connections asynchronously. It is
// called at startup and whenever the user returns from /config so accepting a
// previously-untrusted project takes effect without restarting Lilith.
func (m *ChatModel) connectMCP() uikit.Cmd {
	if m == nil || m.ctx == nil || m.mcpLoading {
		return nil
	}
	settings, _ := config.Load(m.ctx.ConfigDir)
	if !settings.ClaudeCompatibilityEnabled {
		if m.mcpRuntime != nil {
			_ = m.mcpRuntime.Close()
			m.mcpRuntime = nil
		}
		m.mcpSignature = ""
		return nil
	}
	configs := mcp.LoadClaudeConfig(m.ctx.ConfigDir, m.project, config.IsProjectTrusted(settings, m.project))
	mcp.Merge(configs, m.loadClaudePluginMCP())
	sig := mcpConfigSignature(configs)
	if sig == m.mcpSignature && m.mcpRuntime != nil {
		return nil
	}
	if sig == "" {
		if m.mcpRuntime != nil {
			_ = m.mcpRuntime.Close()
			m.mcpRuntime = nil
		}
		m.mcpSignature = ""
		return nil
	}
	m.mcpLoading = true
	base := m.sessionCtx
	if base == nil {
		base = context.Background()
	}
	return func() uikit.Msg {
		ctx, cancel := context.WithTimeout(base, 45*time.Second)
		defer cancel()
		rt := mcp.NewRuntime()
		errs := rt.Connect(ctx, configs)
		return mcpReadyMsg{runtime: rt, signature: sig, errors: errs}
	}
}

func (m *ChatModel) mcpSchemas(mode planstate.Mode) []any {
	if m == nil || m.mcpRuntime == nil {
		return nil
	}
	return m.mcpRuntime.SchemasForMode(mode == planstate.Plan)
}

func (m *ChatModel) callMCP(ctx context.Context, mode planstate.Mode, name string, args map[string]any) (string, error) {
	if m == nil || m.mcpRuntime == nil || !m.mcpRuntime.Has(name) {
		return "", fmt.Errorf("MCP tool unavailable: %s", name)
	}
	if mode == planstate.Plan && !m.mcpRuntime.IsReadOnly(name) {
		return "", fmt.Errorf("Plan mode blocks MCP tool %s because the server does not advertise readOnlyHint", name)
	}
	return m.mcpRuntime.Call(ctx, name, args)
}

func formatMCPErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, "; ")
}
