package tui

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/subagents"
	"github.com/lilith/li/internal/tools"
)

func (m *ChatModel) selectedAgentMode() planstate.Mode {
	if m == nil || m.plans == nil {
		return planstate.Build
	}
	return m.plans.Mode()
}

func (m *ChatModel) effectiveAgentMode() planstate.Mode {
	if m != nil && m.activeTurnID != 0 && m.turnAgentMode != "" {
		return m.turnAgentMode
	}
	return m.selectedAgentMode()
}

func (m *ChatModel) setAgentMode(mode planstate.Mode) bool {
	if m.plans == nil {
		m.plans = planstate.NewManager(nil)
	}
	_, changed, err := m.plans.SetMode(mode)
	if err != nil || !changed {
		m.syncAgentModePresentation()
		return false
	}
	m.syncAgentModePresentation()
	m.invalidateContextUsage()
	if m.activeTurnID != 0 {
		// Do not promote a partial assistant stream into a stable session snapshot
		// merely because Tab changed the NEXT agent. A tiny live checkpoint stores
		// the selected mode while preserving crash-recovery semantics of the turn.
		m.forceLivePersist()
	} else {
		m.persist()
	}
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return true
}

func (m *ChatModel) toggleAgentMode() bool {
	if m.selectedAgentMode() == planstate.Plan {
		return m.setAgentMode(planstate.Build)
	}
	return m.setAgentMode(planstate.Plan)
}

func (m *ChatModel) syncAgentModePresentation() {
	if m.selectedAgentMode() == planstate.Plan {
		m.textarea.Prompt = "plan ❯ "
		m.textarea.Placeholder = "Describe qué quieres planificar…  Tab: Build"
		m.textarea.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
		return
	}
	m.textarea.Prompt = "❯ "
	m.textarea.Placeholder = "Escribe un mensaje…  /help"
	m.textarea.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(m.ctx.Styles.Theme.Primary)
}

func (m *ChatModel) planStatePointer() *planstate.State {
	if m.plans == nil {
		return nil
	}
	state := m.plans.Snapshot()
	return &state
}

func (m *ChatModel) toolEnv(root string, mode planstate.Mode) tools.Env {
	if root == "" {
		root, _ = os.Getwd()
	}
	agentCatalog := m.loadAgents()
	skillCatalog := m.loadSkillsForAgents()
	env := tools.Env{
		Root:      root,
		ConfigDir: m.ctx.ConfigDir,
		Todos:     m.todos,
		Plan:      m.plans,
		AgentMode: mode,
		Agents:    agentCatalog,
	}
	env.RunAgent = func(ctx context.Context, req tools.AgentRequest) (tools.AgentResult, error) {
		providerID, modelID := m.turnProvider, m.turnModel
		if providerID == "" || modelID == "" {
			active := m.ctx.Providers.Active()
			providerID, modelID = active.ProviderID, active.ModelID
		}
		return subagents.Run(ctx, subagents.Config{
			Client: m.ctx.Client, Providers: m.ctx.Providers, ConfigDir: m.ctx.ConfigDir, Root: root,
			ParentProviderID: providerID, ParentModelID: modelID, ParentMode: mode, Skills: skillCatalog,
			Agents: agentCatalog, Depth: 1,
		}, req)
	}
	env.ToolVisible = func(name string, def tools.Definition) bool {
		return planstate.ToolVisible(mode, name, def.Mutating)
	}
	env.ValidateTool = func(name string, def tools.Definition, args map[string]any) error {
		return planstate.ValidateTool(mode, name, def.Mutating, args)
	}
	return env
}

func (m *ChatModel) promptModeBlock(mode planstate.Mode) string {
	if m.plans == nil {
		return ""
	}
	state := m.plans.Snapshot()
	state.Mode = mode
	block := planstate.PromptBlock(state)
	if mode == planstate.Build && strings.TrimSpace(m.turnPlanHandoff) != "" {
		block += planstate.BuildSwitchBlock(m.turnPlanHandoff)
	}
	return block
}

func (m *ChatModel) planWidgetView(_ int) string {
	if m.plans == nil {
		return ""
	}
	state := m.plans.Snapshot()
	// Pending questions have their own compact OpenCode-style dock/launcher.
	// Rendering them here as a second widget would waste precious vertical space.
	if !state.Ready || len(state.Questions) > 0 {
		return ""
	}
	accent := lipgloss.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
	muted := m.ctx.Styles.Muted
	return accent.Render("PLAN LISTO") + muted.Render(" · Tab: Build · /plan show")
}

func isPlanQuestionToolName(name string) bool { return name == "plan_question" }
func isPlanExitToolName(name string) bool     { return name == "plan_exit" }

func (m *ChatModel) rememberToolsForMode(mode planstate.Mode, names []string) []string {
	var cache *[]string
	if mode == planstate.Plan {
		cache = &m.planToolCache
	} else {
		cache = &m.buildToolCache
	}
	for _, name := range names {
		*cache = appendUniqueTool(*cache, name)
	}
	sort.Strings(*cache)
	return append([]string(nil), (*cache)...)
}

func (m *ChatModel) selectToolsForPrompt(text string, mode planstate.Mode) []string {
	env := m.toolEnv("", mode)
	selected := tools.SelectAvailable(text, env)
	if !tools.IsDirectChat(text) && m.skillsEnabled() {
		selected = tools.WithSkillTools(selected, len(m.loadSkills()) > 0)
	}

	// A greeting before any real work should stay schema-free. Once a coding
	// session has materialized tools, however, keep the set additive instead of
	// replacing it on every prompt. This preserves a stable prompt/tool prefix
	// for provider-side caching while tool_search can still add capabilities.
	var cached []string
	if mode == planstate.Plan {
		cached = m.planToolCache
	} else {
		cached = m.buildToolCache
	}
	if tools.IsDirectChat(text) && len(cached) == 0 {
		return nil
	}
	names := m.rememberToolsForMode(mode, selected)
	return tools.FilterAvailable(names, env)
}

func (m *ChatModel) todoBlockForMode(mode planstate.Mode) string {
	if mode == planstate.Plan {
		return ""
	}
	return m.todoBlock()
}
