package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/hooks"
	"github.com/lilith/li/internal/mcp"
	claudeplugins "github.com/lilith/li/internal/plugins"
	"github.com/lilith/li/internal/skills"
)

func (m *ChatModel) claudePlugins() []claudeplugins.Plugin {
	if m == nil || m.ctx == nil {
		return nil
	}
	settings, _ := config.Load(m.ctx.ConfigDir)
	if !settings.ClaudeCompatibilityEnabled {
		return nil
	}
	return claudeplugins.Discover(m.ctx.ConfigDir, m.project, config.IsProjectTrusted(settings, m.project))
}

func (m *ChatModel) loadClaudePluginAgents() []agents.Agent {
	var out []agents.Agent
	for _, plugin := range m.claudePlugins() {
		out = append(out, claudeplugins.LoadAgents(plugin)...)
	}
	return out
}

func (m *ChatModel) loadClaudePluginSkills() []skills.Skill {
	var out []skills.Skill
	for _, plugin := range m.claudePlugins() {
		out = append(out, claudeplugins.LoadSkills(plugin)...)
	}
	return out
}

func (m *ChatModel) loadClaudePluginHooks() *hooks.Runner {
	root := m.project
	out := &hooks.Runner{Root: root, Entries: map[string][]hooks.Matcher{}}
	for _, plugin := range m.claudePlugins() {
		for _, path := range plugin.HookFiles {
			runner := hooks.LoadFile(path, root)
			runner.SetPluginContext(plugin.Root, plugin.DataDir, "")
			out.Merge(runner)
		}
		if len(plugin.HooksInline) > 0 {
			runner := hooks.ParseJSON(plugin.HooksInline, root)
			runner.SetPluginContext(plugin.Root, plugin.DataDir, "")
			out.Merge(runner)
		}
	}
	return out
}

func (m *ChatModel) loadClaudePluginMCP() map[string]mcp.ServerConfig {
	out := map[string]mcp.ServerConfig{}
	for _, plugin := range m.claudePlugins() {
		raw := map[string]mcp.ServerConfig{}
		for _, path := range plugin.MCPFiles {
			mcp.Merge(raw, mcp.LoadConfigFile(path))
		}
		if len(plugin.MCPInline) > 0 {
			mcp.Merge(raw, mcp.ParseConfig(plugin.MCPInline))
		}
		if len(raw) == 0 {
			continue
		}
		if plugin.DataDir != "" {
			_ = os.MkdirAll(plugin.DataDir, 0o700)
		}
		mcp.Merge(out, mcp.ScopePluginConfigs(plugin.Name, plugin.Root, plugin.DataDir, m.project, raw))
	}
	return out
}

func (m *ChatModel) runPluginsCommand() uikit.Cmd {
	list := m.claudePlugins()
	if len(list) == 0 {
		m.AddSystem("No hay plugins locales Claude detectados. Se buscan manifests .claude-plugin/plugin.json dentro de ~/.claude/skills y .claude/skills; los plugins del proyecto requieren confianza.")
		return nil
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Source != list[j].Source {
			return list[i].Source < list[j].Source
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	var b strings.Builder
	b.WriteString("Plugins locales Claude detectados:")
	for _, plugin := range list {
		version := ""
		if plugin.Version != "" {
			version = " · v" + plugin.Version
		}
		hooksCount := len(plugin.HookFiles)
		if len(plugin.HooksInline) > 0 {
			hooksCount++
		}
		mcpCount := len(plugin.MCPFiles)
		if len(plugin.MCPInline) > 0 {
			mcpCount++
		}
		fmt.Fprintf(&b, "\n- %s%s [%s] · skills=%d · commands=%d · agents=%d · hooks=%d · mcp=%d", plugin.Name, version, plugin.Source, len(plugin.SkillDirs), len(plugin.CommandDirs), len(plugin.AgentDirs), hooksCount, mcpCount)
	}
	b.WriteString("\n\nSkills, commands y agentes usan namespace plugin:nombre; los servidores MCP usan mcp__plugin_<plugin>_<server>__<tool>. LSP, monitors, workflows y marketplace siguen fuera de este bloque.")
	m.AddSystem(b.String())
	return nil
}

func (m *ChatModel) runReloadPluginsCommand() uikit.Cmd {
	plugins := m.claudePlugins()
	agents := m.loadClaudePluginAgents()
	skills := m.loadClaudePluginSkills()
	hookCount := m.loadClaudePluginHooks().Count()
	mcpCount := len(m.loadClaudePluginMCP())
	m.AddSystem(fmt.Sprintf("Plugins locales recargados: %d plugins · %d skills/commands · %d agentes · %d hooks · %d servidores MCP. Los cambios aplican al siguiente turno; MCP reconecta sólo si cambió su configuración.", len(plugins), len(skills), len(agents), hookCount, mcpCount))
	return m.connectMCP()
}
