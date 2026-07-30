package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/hooks"
)

func (m *ChatModel) hookRunner() *hooks.Runner {
	if m == nil || m.ctx == nil {
		return &hooks.Runner{}
	}
	s, _ := config.Load(m.ctx.ConfigDir)
	if !s.HooksEnabled {
		return &hooks.Runner{}
	}
	base := hooks.Load(m.ctx.ConfigDir, m.project, config.IsProjectTrusted(s, m.project))
	base.Merge(m.loadClaudePluginHooks())
	base.MCPTool = func(ctx context.Context, server, tool string, input map[string]any) (string, error) {
		if m.mcpRuntime == nil {
			return "", fmt.Errorf("MCP aún no está conectado")
		}
		return m.mcpRuntime.CallServerTool(ctx, server, tool, input)
	}
	return base
}

func (m *ChatModel) toolHookRunner() *hooks.Runner {
	base := m.hookRunner()
	if m != nil && m.turnSkillHooks != nil {
		base.Merge(m.turnSkillHooks)
	}
	return base
}

func claudeToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "run_terminal_command":
		return "Bash"
	case "read_files":
		return "Read"
	case "glob", "list_directory":
		return "Glob"
	case "code_search":
		return "Grep"
	case "create_file":
		return "Write"
	case "str_replace", "apply_diff":
		return "Edit"
	case "read_url":
		return "WebFetch"
	case "web_search":
		return "WebSearch"
	case "agent", "task":
		return "Agent"
	case "todo_write":
		return "TodoWrite"
	default:
		return name
	}
}

func (m *ChatModel) hookInput(event string) map[string]any {
	sessionID := ""
	if m.sess != nil {
		sessionID = m.sess.ID
	}
	return map[string]any{
		"session_id":      sessionID,
		"cwd":             m.project,
		"permission_mode": string(m.effectiveAgentMode()),
		"hook_event_name": event,
	}
}

func (m *ChatModel) runUserPromptHooks(prompt string) (string, error) {
	runner := m.hookRunner()
	if runner.Count() == 0 {
		return prompt, nil
	}
	input := m.hookInput("UserPromptSubmit")
	input["prompt"] = prompt
	res, err := runner.Run(context.Background(), "UserPromptSubmit", "", input)
	if err != nil {
		return "", err
	}
	if res.Blocked {
		return "", fmt.Errorf("prompt bloqueado por hook: %s", res.Reason)
	}
	if res.AdditionalContext != "" {
		prompt += "\n\n<hook_context>\n" + res.AdditionalContext + "\n</hook_context>"
	}
	if res.SystemMessage != "" {
		m.AddSystem(res.SystemMessage)
	}
	return prompt, nil
}

func (m *ChatModel) runSessionHook(event string) {
	runner := m.hookRunner()
	if runner.Count() == 0 {
		return
	}
	_, _ = runner.Run(context.Background(), event, "", m.hookInput(event))
}
