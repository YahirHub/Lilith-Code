package tui

import (
	"fmt"
	"strings"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/moduleapi"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/tui/uikit"
)

// moduleHost is the only adapter between stable moduleapi and TUI internals.
// Private modules receive moduleapi.Host, never *ChatModel, which keeps their
// source isolated from routine refactors in the public branch.
type moduleHost struct {
	ctx      *AppContext
	chat     *ChatModel
	registry *moduleapi.Registry
	cmd      uikit.Cmd
}

var (
	_ moduleapi.Host                = (*moduleHost)(nil)
	_ moduleapi.SkillInvoker        = (*moduleHost)(nil)
	_ moduleapi.Submitter           = (*moduleHost)(nil)
	_ moduleapi.ScreenOpener        = (*moduleHost)(nil)
	_ moduleapi.RewindState         = (*moduleHost)(nil)
	_ moduleapi.ProjectInitializer  = (*moduleHost)(nil)
	_ moduleapi.GoalController      = (*moduleHost)(nil)
	_ moduleapi.AgentModeController = (*moduleHost)(nil)
	_ moduleapi.Compactor           = (*moduleHost)(nil)
	_ moduleapi.SessionForker       = (*moduleHost)(nil)
	_ moduleapi.MemoryController    = (*moduleHost)(nil)
	_ moduleapi.MCPController       = (*moduleHost)(nil)
	_ moduleapi.AgentController     = (*moduleHost)(nil)
	_ moduleapi.PluginController    = (*moduleHost)(nil)
	_ moduleapi.SessionController   = (*moduleHost)(nil)
)

func newModuleHost(ctx *AppContext, chat *ChatModel, registry *moduleapi.Registry) *moduleHost {
	return &moduleHost{ctx: ctx, chat: chat, registry: registry}
}

func (h *moduleHost) addCmd(cmd uikit.Cmd) {
	if cmd == nil {
		return
	}
	if h.cmd == nil {
		h.cmd = cmd
		return
	}
	h.cmd = uikit.Batch(h.cmd, cmd)
}

func (h *moduleHost) ConfigDir() string {
	if h.ctx == nil {
		return ""
	}
	return h.ctx.ConfigDir
}

func (h *moduleHost) ProjectRoot() string {
	if h.chat == nil {
		return ""
	}
	return h.chat.project
}

func (h *moduleHost) AddSystem(text string) {
	if h.chat != nil {
		h.chat.AddSystem(text)
	}
}

func (h *moduleHost) AddError(text string) {
	if h.chat != nil {
		h.chat.AddError(text)
	}
}

func (h *moduleHost) InvokeSkill(name, args string) {
	if h.chat == nil {
		return
	}
	h.addCmd(h.chat.invokeSkill(name, args))
}

func (h *moduleHost) Submit(text string) {
	if h.chat == nil || strings.TrimSpace(text) == "" {
		return
	}
	_, cmd := h.chat.submit(text)
	h.addCmd(cmd)
}

func (h *moduleHost) OpenScreen(screen moduleapi.Screen) {
	if h.ctx == nil {
		return
	}
	switch screen {
	case moduleapi.ScreenHelp:
		h.addCmd(switchTo(NewHelpScreen(h.ctx)))
	case moduleapi.ScreenOnboarding:
		h.addCmd(switchTo(NewOnboarding(h.ctx, false)))
	case moduleapi.ScreenProviders:
		h.addCmd(switchTo(NewProviderScreen(h.ctx)))
	case moduleapi.ScreenModels:
		h.addCmd(switchTo(NewModelSelector(h.ctx)))
	case moduleapi.ScreenConfig:
		h.addCmd(switchTo(NewConfigScreen(h.ctx)))
	case moduleapi.ScreenHistory:
		if h.chat == nil {
			h.AddError("El historial requiere una conversación activa.")
			return
		}
		h.addCmd(switchTo(NewHistory(h.ctx, h.chat.store, h.chat.project)))
	case moduleapi.ScreenRewind:
		h.addCmd(switchTo(NewRewindScreen(h.ctx, h.chat)))
	default:
		h.AddError("El módulo solicitó una pantalla no soportada por esta versión de Lilith: " + string(screen))
	}
}

func (h *moduleHost) RewindBusy() bool {
	return h.chat != nil && h.chat.rewindSessionBusy()
}

func (h *moduleHost) ModuleStatuses() []moduleapi.Status {
	if h.registry == nil {
		return nil
	}
	return h.registry.Statuses()
}

func (h *moduleHost) InitializeProject() {
	if h.chat != nil {
		h.addCmd(h.chat.runInit())
	}
}

func (h *moduleHost) RunGoal(args string) {
	if h.chat != nil {
		h.addCmd(h.chat.runGoalCommand(args))
	}
}

func (h *moduleHost) AgentMode() string {
	if h.chat == nil {
		return string(planstate.Build)
	}
	return string(h.chat.selectedAgentMode())
}

func (h *moduleHost) SetAgentMode(mode string) {
	if h.chat == nil {
		return
	}
	var next planstate.Mode
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(planstate.Build):
		next = planstate.Build
	case string(planstate.Plan):
		next = planstate.Plan
	case string(planstate.Goal):
		next = planstate.Goal
	default:
		h.AddError("Modo de agente no soportado: " + mode)
		return
	}
	h.chat.setAgentMode(next)
}

func (h *moduleHost) SyncAgentModeUI() {
	if h.chat != nil {
		h.addCmd(h.chat.chatMouseModeCmd())
	}
}

func (h *moduleHost) LatestPlan() string {
	if h.chat == nil || h.chat.plans == nil {
		return ""
	}
	return strings.TrimSpace(h.chat.plans.LatestPlan())
}

func (h *moduleHost) Compact(instructions string) {
	if h.chat != nil {
		h.addCmd(h.chat.runCompactCommand(instructions))
	}
}

func (h *moduleHost) ForkSession(title string) {
	if h.chat != nil {
		h.addCmd(h.chat.runForkSessionCommand(title))
	}
}

func (h *moduleHost) MemorySummary() string {
	if h.chat == nil {
		return "Memoria no disponible en este host."
	}
	return h.chat.memorySummary()
}

func (h *moduleHost) SetAutoMemory(enabled bool) error {
	if h.ctx == nil || h.chat == nil {
		return fmt.Errorf("memoria no disponible en este host")
	}
	settings, _ := config.Load(h.ctx.ConfigDir)
	settings.AutoMemoryEnabled = enabled
	if err := config.Save(h.ctx.ConfigDir, settings); err != nil {
		return err
	}
	h.chat.invalidateContextUsage()
	return nil
}

func (h *moduleHost) RunMCP(args string) {
	if h.chat != nil {
		h.addCmd(h.chat.runMCPCommand(args))
	}
}

func (h *moduleHost) RunTasks() {
	if h.chat != nil {
		h.addCmd(h.chat.runTasksCommand())
	}
}

func (h *moduleHost) RunSubtask(args string) {
	if h.chat != nil {
		h.addCmd(h.chat.runForkCommand(args))
	}
}

func (h *moduleHost) Agents() []moduleapi.AgentInfo {
	if h.chat == nil {
		return nil
	}
	list := h.chat.loadAgents()
	out := make([]moduleapi.AgentInfo, 0, len(list))
	for _, a := range list {
		out = append(out, moduleapi.AgentInfo{
			Name:        a.Name,
			Description: a.Description,
			Source:      a.Source,
			Hidden:      a.Hidden,
		})
	}
	return out
}

func (h *moduleHost) RunPlugins() {
	if h.chat != nil {
		h.addCmd(h.chat.runPluginsCommand())
	}
}

func (h *moduleHost) ReloadPlugins() {
	if h.chat != nil {
		h.addCmd(h.chat.runReloadPluginsCommand())
	}
}

func (h *moduleHost) ClearConversation() {
	if h.chat == nil {
		return
	}
	h.chat.Clear()
	h.addCmd(uikit.DisableMouse)
}

func (h *moduleHost) ExitApplication() {
	if h.chat != nil {
		if h.chat.activeTurnID != 0 {
			h.chat.cancelTurn()
		}
		h.chat.runSessionHook("SessionEnd")
	}
	h.addCmd(uikit.Quit)
}
