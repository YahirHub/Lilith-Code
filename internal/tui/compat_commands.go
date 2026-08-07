package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/config"
)

func (m *ChatModel) memorySummary() string {
	settings, _ := config.Load(m.ctx.ConfigDir)
	state := "OFF"
	if settings.AutoMemoryEnabled {
		state = "ON"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Memoria automática: %s\nDirectorio: %s", state, m.mainMemoryDir())
	bundle := m.instructionBundle()
	if docs := bundle.Sources(); len(docs) > 0 {
		b.WriteString("\n\nInstrucciones detectadas:")
		for _, doc := range docs {
			fmt.Fprintf(&b, "\n- %s [%s]", doc.Path, doc.Kind)
		}
	}
	return b.String()
}

func (m *ChatModel) runMCPCommand(args string) uikit.Cmd {
	args = strings.ToLower(strings.TrimSpace(args))
	if args == "reload" || args == "reconnect" {
		m.mcpSignature = ""
		m.AddSystem("Reconectando servidores MCP configurados…")
		return m.connectMCP()
	}
	if m.mcpRuntime == nil {
		m.AddSystem("MCP: no hay servidores conectados. Configura ~/.claude.json o .mcp.json; los archivos del proyecto requieren confianza.")
		return nil
	}
	all := m.mcpRuntime.Tools()
	if len(all) == 0 {
		m.AddSystem("MCP conectado, sin herramientas publicadas.")
		return nil
	}
	byServer := map[string][]string{}
	for _, tool := range all {
		label := tool.FullName
		if tool.ReadOnly {
			label += " [read-only]"
		}
		byServer[tool.Server] = append(byServer[tool.Server], label)
	}
	servers := make([]string, 0, len(byServer))
	for name := range byServer {
		servers = append(servers, name)
	}
	sort.Strings(servers)
	var b strings.Builder
	b.WriteString("MCP conectado:")
	for _, server := range servers {
		fmt.Fprintf(&b, "\n\n%s", server)
		for _, tool := range byServer[server] {
			b.WriteString("\n  - " + tool)
		}
	}
	b.WriteString("\n\nUsa /mcp reload para reconectar después de cambiar configuración.")
	m.AddSystem(b.String())
	return nil
}

func (m *ChatModel) runTasksCommand() uikit.Cmd {
	if len(m.agentPanels) == 0 {
		m.AddSystem("No hay tareas de subagentes registradas en esta sesión.")
		return nil
	}
	panels := make([]*AgentPanel, 0, len(m.agentPanels))
	for _, panel := range m.agentPanels {
		if panel != nil {
			panels = append(panels, panel)
		}
	}
	sort.SliceStable(panels, func(i, j int) bool { return panels[i].StartedAt.Before(panels[j].StartedAt) })
	var b strings.Builder
	b.WriteString("Tareas de subagentes:")
	for _, panel := range panels {
		status := agentStatusLabel(panel.Status)
		mode := ""
		if panel.Background {
			mode = " · background"
		}
		fmt.Fprintf(&b, "\n- @%s · %s%s · %s", panel.Name, status, mode, panel.TaskID)
		if panel.ParentTaskID != "" {
			b.WriteString(" · padre=" + panel.ParentTaskID)
		}
	}
	m.AddSystem(b.String())
	return nil
}

func (m *ChatModel) runForkCommand(args string) uikit.Cmd {
	if strings.TrimSpace(os.Getenv("CLAUDE_CODE_FORK_SUBAGENT")) == "0" {
		m.AddSystem("Los forks están desactivados por CLAUDE_CODE_FORK_SUBAGENT=0.")
		return nil
	}
	raw := strings.TrimSpace(args)
	if raw == "" {
		m.AddSystem("Uso: /subtask [--foreground] [--worktree] <tarea>")
		return nil
	}
	background := true
	isolation := ""
	var promptParts []string
	for _, field := range strings.Fields(raw) {
		switch strings.ToLower(field) {
		case "--foreground", "--fg":
			background = false
		case "--background", "--bg":
			background = true
		case "--worktree":
			isolation = "worktree"
		default:
			promptParts = append(promptParts, field)
		}
	}
	prompt := strings.TrimSpace(strings.Join(promptParts, " "))
	if prompt == "" {
		m.AddSystem("Uso: /subtask [--foreground] [--worktree] <tarea>")
		return nil
	}
	visible := "/subtask " + raw
	a := agents.Agent{Name: "fork", Description: "Conversation fork", Model: "default"}
	_, cmd := m.invokeForkDefinitionWithBackground(a, prompt, visible, "conversation fork", background, isolation)
	return cmd
}
