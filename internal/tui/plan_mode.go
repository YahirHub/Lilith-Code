package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	planstate "github.com/lilith/li/internal/plan"
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
	env := tools.Env{
		Root:      root,
		ConfigDir: m.ctx.ConfigDir,
		Todos:     m.todos,
		Plan:      m.plans,
		AgentMode: mode,
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

func (m *ChatModel) planWidgetView(w int) string {
	if m.plans == nil {
		return ""
	}
	state := m.plans.Snapshot()
	if !state.Ready && len(state.Questions) == 0 {
		return ""
	}
	boxWidth := w - 2
	if boxWidth < 10 {
		boxWidth = w
	}
	accent := lipgloss.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
	muted := m.ctx.Styles.Muted
	var lines []string
	if state.Ready {
		lines = append(lines, accent.Render("PLAN LISTO")+muted.Render(" · Tab para Build · /plan show"))
	} else {
		lines = append(lines, accent.Render("PLAN · NECESITA DECISIÓN"))
		for i, q := range state.Questions {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, truncateOneLine(q.Question, boxWidth-5)))
			if len(q.Options) > 0 {
				options := make([]string, 0, len(q.Options))
				for j, option := range q.Options {
					label := option.Label
					if option.Description != "" {
						label += " — " + option.Description
					}
					options = append(options, fmt.Sprintf("%d) %s", j+1, label))
				}
				lines = append(lines, muted.Render("   "+truncateOneLine(strings.Join(options, " · "), boxWidth-4)))
			}
		}
		lines = append(lines, muted.Render("Responde normalmente en el editor."))
	}
	return lipgloss.NewStyle().Width(boxWidth).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func isPlanQuestionToolName(name string) bool { return name == "plan_question" }
func isPlanExitToolName(name string) bool     { return name == "plan_exit" }

func (m *ChatModel) selectToolsForPrompt(text string, mode planstate.Mode) []string {
	env := m.toolEnv("", mode)
	names := tools.SelectAvailable(text, env)
	if !tools.IsDirectChat(text) && m.skillsEnabled() {
		names = tools.WithSkillTools(names, len(m.loadSkills()) > 0)
	}
	return tools.FilterAvailable(names, env)
}

func (m *ChatModel) todoBlockForMode(mode planstate.Mode) string {
	if mode == planstate.Plan {
		return ""
	}
	return m.todoBlock()
}
