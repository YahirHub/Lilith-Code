package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/subagents"
	"github.com/lilith/li/internal/tools"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
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

var primaryAgentModes = []planstate.Mode{planstate.Build, planstate.Plan, planstate.Goal}

func (m *ChatModel) cycleAgentMode(delta int) bool {
	current := m.selectedAgentMode()
	index := 0
	for i, mode := range primaryAgentModes {
		if mode == current {
			index = i
			break
		}
	}
	index = (index + delta) % len(primaryAgentModes)
	if index < 0 {
		index += len(primaryAgentModes)
	}
	return m.setAgentMode(primaryAgentModes[index])
}

func (m *ChatModel) syncAgentModePresentation() {
	switch m.selectedAgentMode() {
	case planstate.Plan:
		m.textarea.Prompt = "plan ❯ "
		m.textarea.Placeholder = "Describe qué quieres planificar…  Tab: Goal"
		m.textarea.FocusedStyle.Prompt = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
	case planstate.Goal:
		m.textarea.Prompt = "goal ❯ "
		m.textarea.Placeholder = "Define el objetivo persistente…  Tab: Build"
		m.textarea.FocusedStyle.Prompt = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Success).Bold(true)
	default:
		m.textarea.Prompt = "build ❯ "
		m.textarea.Placeholder = "Escribe un mensaje…  Tab: Plan"
		m.textarea.FocusedStyle.Prompt = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Primary).Bold(true)
	}
}

func (m *ChatModel) planStatePointer() *planstate.State {
	if m.plans == nil {
		return nil
	}
	state := m.plans.Snapshot()
	return &state
}

func (m *ChatModel) toolEnv(root string, mode planstate.Mode) tools.Env {
	return m.toolEnvWithAgentEvents(root, mode, nil)
}

func (m *ChatModel) toolEnvWithAgentEvents(root string, mode planstate.Mode, events subagents.EventSink) tools.Env {
	if root == "" {
		root, _ = os.Getwd()
	}
	agentCatalog := m.loadAgents()
	skillCatalog := m.loadSkillsForAgents()
	providerID, modelID := m.turnProvider, m.turnModel
	if providerID == "" || modelID == "" {
		active := m.ctx.Providers.Active()
		providerID, modelID = active.ProviderID, active.ModelID
	}
	parentMessages := m.forkContextMessages(mode)
	parentTools := append([]string(nil), m.activeTools...)
	env := tools.Env{
		Root:      root,
		ConfigDir: m.ctx.ConfigDir,
		Todos:     m.todos,
		MemoryDir: m.mainMemoryDir(),
		Plan:      m.plans,
		Goal:      m.goals,
		AgentMode: mode,
		Agents:    agentCatalog,
	}
	env.RunAgent = func(ctx context.Context, req tools.AgentRequest) (tools.AgentResult, error) {
		cfg := subagents.Config{
			Client: m.ctx.Client, Providers: m.ctx.Providers, ConfigDir: m.ctx.ConfigDir, Root: root, StoreProject: m.project,
			ParentProviderID: providerID, ParentModelID: modelID, ParentMode: mode,
			ParentMessages: parentMessages, ParentToolNames: parentTools, Skills: skillCatalog,
			Agents: agentCatalog, Depth: 1, Events: events, BackgroundContext: m.sessionCtx, ParentMCP: m.mcpRuntime,
			PluginHooks: m.loadClaudePluginHooks(),
		}
		if req.Background && backgroundTasksAllowed() {
			return subagents.StartBackground(cfg, req)
		}
		return subagents.Run(ctx, cfg, req)
	}
	env.BeforeTool = func(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
		runner := m.toolHookRunner()
		// Capture before either the tool or any Pre/PostToolUse hook can mutate
		// the workspace. Hooks are external commands, so even a nominally
		// read-only tool needs a checkpoint when hooks are configured. A capture
		// failure is recorded but does not block the user's work; /rewind still
		// offers conversation-only restoration.
		if m.rewindToolMayMutate(name) || runner.Count() > 0 {
			_ = m.ensureActiveRewindWorkspace()
		}
		if runner.Count() == 0 {
			return args, nil
		}
		input := m.hookInput("PreToolUse")
		input["tool_name"] = claudeToolName(name)
		input["tool_input"] = args
		res, err := runner.Run(ctx, "PreToolUse", claudeToolName(name), input)
		if err != nil {
			return nil, err
		}
		if res.Blocked {
			return nil, fmt.Errorf("tool bloqueada por hook: %s", res.Reason)
		}
		if res.SystemMessage != "" { /* delivered through logs/transcript only when safe */
		}
		if res.UpdatedInput != nil {
			return res.UpdatedInput, nil
		}
		return args, nil
	}
	env.AfterTool = func(ctx context.Context, name string, args map[string]any, output string, runErr error) (string, error) {
		runner := m.toolHookRunner()
		if runner.Count() == 0 {
			return output, nil
		}
		event := "PostToolUse"
		if runErr != nil {
			event = "PostToolUseFailure"
		}
		input := m.hookInput(event)
		input["tool_name"] = claudeToolName(name)
		input["tool_input"] = args
		if runErr != nil {
			input["error"] = runErr.Error()
		} else {
			input["tool_response"] = output
		}
		res, err := runner.Run(ctx, event, claudeToolName(name), input)
		if err != nil {
			return output, err
		}
		if res.Blocked {
			return output, fmt.Errorf("resultado bloqueado por hook: %s", res.Reason)
		}
		if res.UpdatedOutput != "" {
			return res.UpdatedOutput, nil
		}
		if res.AdditionalContext != "" {
			output += "\n\n<hook_context>\n" + res.AdditionalContext + "\n</hook_context>"
		}
		return output, nil
	}
	env.ToolVisible = func(name string, def tools.Definition) bool {
		return !m.toolDeniedForTurn(name) && planstate.ToolVisible(mode, name, def.Mutating)
	}
	env.ValidateTool = func(name string, def tools.Definition, args map[string]any) error {
		if m.toolDeniedForTurn(name) {
			return fmt.Errorf("tool %s is disabled by the active skill", name)
		}
		return planstate.ValidateTool(mode, name, def.Mutating, args)
	}
	env.DynamicTool = func(ctx context.Context, name string, args map[string]any) (string, error) {
		if m.toolDeniedForTurn(name) {
			return "", fmt.Errorf("tool %s is disabled by the active skill", name)
		}
		return m.callMCP(ctx, mode, name, args)
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
	accent := tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
	muted := m.ctx.Styles.Muted
	return accent.Render("PLAN LISTO") + muted.Render(" · Shift+Tab: Build · Tab: Goal · /plan show")
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
	if !tools.IsDirectChat(text) && env.MemoryDir != "" {
		selected = appendUniqueTool(selected, "memory_read")
		if mode != planstate.Plan {
			selected = appendUniqueTool(selected, "memory_write")
		}
	}
	if !tools.IsDirectChat(text) && m.skillsEnabled() {
		selected = tools.WithSkillTools(selected, len(m.loadSkills()) > 0)
	}
	if !tools.IsDirectChat(text) && m.goals != nil {
		if state := m.goals.Snapshot(); state != nil {
			for _, name := range []string{"get_goal", "update_goal"} {
				if _, ok := tools.Get(name); ok {
					selected = appendUniqueTool(selected, name)
				}
			}
			// Recreating an already-active objective was the source of a provider
			// loop where create_goal appeared on every continuation. Only expose it
			// again after the previous goal is complete; /goal remains the explicit
			// user-controlled way to replace an active objective.
			if state.Status == ligoal.Complete {
				if _, ok := tools.Get("create_goal"); ok {
					selected = appendUniqueTool(selected, "create_goal")
				}
			}
		}
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
