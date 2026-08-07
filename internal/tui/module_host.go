package tui

import (
	"strings"

	"github.com/lilith/li/internal/moduleapi"
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
	_ moduleapi.Host         = (*moduleHost)(nil)
	_ moduleapi.SkillInvoker = (*moduleHost)(nil)
	_ moduleapi.Submitter    = (*moduleHost)(nil)
	_ moduleapi.ScreenOpener = (*moduleHost)(nil)
	_ moduleapi.RewindState  = (*moduleHost)(nil)
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
