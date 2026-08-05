package tui

import (
	"context"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/interaction"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
)

// AppContext is shared runtime state passed to every screen.
type AppContext struct {
	ConfigDir string
	Providers providers.Config
	Client    *openai.Client
	Styles    Styles
	Width     int
	Height    int
	FirstRun  bool
	// Resume es la conversación a reanudar al arrancar (`li --continue`).
	Resume *session.Session
	// Interactions carries local confirmations and masked secrets from tools to the TUI.
	Interactions *interaction.Bridge
}

// ReloadProviders refreshes AppContext.Providers from disk + bundled catalog.
func (c *AppContext) ReloadProviders() error {
	cfg, err := providers.LoadWithBundled(c.ConfigDir)
	if err != nil {
		return err
	}
	c.Providers = cfg
	return nil
}

// SelectOpenCodeFree activates the bundled OpenCode Free provider (first model).
func (c *AppContext) SelectOpenCodeFree() error {
	if err := c.ReloadProviders(); err != nil {
		return err
	}
	p := c.Providers.FindProvider(providers.OpenCodeFreeID)
	if p == nil || len(p.Models) == 0 {
		return nil
	}
	if err := providers.SetActive(c.ConfigDir, p.ID, p.Models[0].ID); err != nil {
		return err
	}
	return c.ReloadProviders()
}

// -----------------------------------------------------------------------------
// Screen router
// -----------------------------------------------------------------------------

type switchScreenMsg struct{ next uikit.Model }
type systemMsg struct{ text string }
type errMsg struct{ err error }

func switchTo(m uikit.Model) uikit.Cmd { return func() uikit.Msg { return switchScreenMsg{next: m} } }
func switchToChat() uikit.Cmd          { return func() uikit.Msg { return switchScreenMsg{next: nil} } } // nil = go to chat
func switchToChatWithSystem(s string) uikit.Cmd {
	return uikit.Batch(switchToChat(), func() uikit.Msg { return systemMsg{text: s} })
}
func showError(err error) uikit.Cmd { return func() uikit.Msg { return errMsg{err: err} } }

// RootModel wraps whichever screen is active.
type RootModel struct {
	ctx                 *AppContext
	current             uikit.Model
	chat                *ChatModel // persistent chat model
	cancel              context.CancelFunc
	interactionPrevious uikit.Model
}

// chatRuntimeMsg reports whether msg belongs to the persistent chat runtime
// rather than to whichever screen happens to be visible. Provider chunks,
// tool results and animation/persistence ticks must keep reaching ChatModel
// while /config, /models, /providers, etc. are open; otherwise the command
// chain that pumps the stream is lost and the turn appears frozen on return.
func chatRuntimeMsg(msg uikit.Msg) bool {
	switch msg.(type) {
	case thinkingTickMsg,
		transcriptRefreshTickMsg,
		livePersistDoneMsg,
		livePersistTickMsg,
		cmdElapsedTickMsg,
		chatStreamMsg,
		compactionResultMsg,
		toolResultsMsg,
		manualAgentResultMsg,
		agentEventBatchMsg,
		agentEventStreamDoneMsg,
		mcpReadyMsg,
		bashResultMsg:
		return true
	default:
		return false
	}
}

func (m RootModel) chatVisible() bool {
	chat, ok := m.current.(*ChatModel)
	return ok && chat == m.chat
}

func (m *ChatModel) wantsMouseCapture() bool {
	return m != nil && (m.hasPendingPermission() || m.hasPendingPlanQuestions() || m.todoExpandable())
}

// chatMouseModeCmd enables mouse reporting only when chat currently exposes a
// clickable inline control. Most of the time reporting remains disabled so the
// terminal keeps native text selection/copy behavior. A large TodoWrite also
// opts in because its visible block can be clicked to expand/collapse.
func (m *ChatModel) chatMouseModeCmd() uikit.Cmd {
	if m.wantsMouseCapture() {
		return uikit.EnableMouseCellMotion
	}
	return uikit.DisableMouse
}

// wantsMouseCapture reports the logical mouse mode independently from the
// terminal backend. Lilith uses it through mouseModeCmd; the tview/Tcell
// runtime reads the same value when publishing each complete frame.
func (m RootModel) wantsMouseCapture() bool {
	if m.chatVisible() {
		return m.chat.wantsMouseCapture()
	}
	return true
}

// mouseModeCmd keeps click support on settings/select screens but releases
// mouse capture in ordinary chat.
func (m RootModel) mouseModeCmd() uikit.Cmd {
	if m.wantsMouseCapture() {
		return uikit.EnableMouseCellMotion
	}
	return uikit.DisableMouse
}

// NewRootModel builds the persistent screen router consumed by the tview
// runtime adapter. If firstRun is true, onboarding is shown; otherwise chat
// opens directly.
func NewRootModel(ctx *AppContext) RootModel {
	chat := NewChat(ctx)
	m := RootModel{ctx: ctx, chat: chat}
	if ctx.Resume != nil {
		chat.LoadSession(ctx.Resume)
	}
	if ctx.FirstRun {
		m.current = NewOnboarding(ctx, true)
	} else {
		m.current = chat
	}
	return m
}

func (m RootModel) Init() uikit.Cmd {
	if m.current == nil {
		return waitInteractionCmd(m.ctx.Interactions)
	}
	var screenCmd uikit.Cmd
	// Auxiliary screens start with logical mouse capture in both terminal backends.
	// Only chat needs an Init-time override so native text selection stays active.
	if m.chatVisible() {
		screenCmd = uikit.Batch(m.current.Init(), m.mouseModeCmd())
	} else {
		screenCmd = m.current.Init()
	}
	return uikit.Batch(screenCmd, waitInteractionCmd(m.ctx.Interactions))
}

func (m RootModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case interactionRequestMsg:
		if v.request == nil {
			return m, waitInteractionCmd(m.ctx.Interactions)
		}
		if v.request.Canceled() {
			v.request.Resolve(interaction.Result{Canceled: true})
			return m, waitInteractionCmd(m.ctx.Interactions)
		}
		if m.current != m.chat {
			m.interactionPrevious = m.current
		} else {
			m.interactionPrevious = nil
		}
		m.current = m.chat
		return m, uikit.Batch(m.chat.openPermission(v.request), m.mouseModeCmd())
	case interactionResolvedMsg:
		if v.request != nil {
			v.request.Resolve(v.result)
		}
		if m.interactionPrevious != nil {
			m.current = m.interactionPrevious
			m.interactionPrevious = nil
		} else {
			m.current = m.chat
		}
		return m, uikit.Batch(m.mouseModeCmd(), waitInteractionCmd(m.ctx.Interactions))
	case uikit.WindowSizeMsg:
		m.ctx.Width = v.Width
		m.ctx.Height = v.Height
		if m.chat != nil {
			m.chat.Resize(v.Width, v.Height)
		}
	case switchScreenMsg:
		if v.next == nil {
			// Return to chat
			m.chat.invalidateContextUsage()
			m.current = m.chat
			return m, uikit.Batch(m.mouseModeCmd(), m.chat.connectMCP())
		}
		m.current = v.next
		return m, uikit.Batch(m.current.Init(), m.mouseModeCmd())
	case systemMsg:
		m.chat.AddSystem(v.text)
		return m, nil
	case errMsg:
		m.chat.AddError(v.err.Error())
		return m, nil
	case modelCatalogRefreshedMsg:
		// The refresh command may finish after the user leaves /models. Its cache
		// and custom-provider updates are already persisted; reload instead of
		// installing the command's potentially stale config snapshot.
		if _, visible := m.current.(ModelSelectorModel); !visible {
			_ = m.ctx.ReloadProviders()
			m.chat.invalidateContextUsage()
			return m, nil
		}
		m.ctx.Providers = v.config
		m.chat.invalidateContextUsage()
		next, cmd := m.current.Update(v)
		m.current = next
		return m, cmd
	case resumeSessionMsg:
		m.chat.LoadSession(v.sess)
		m.current = m.chat
		return m, uikit.Batch(m.mouseModeCmd(), m.chat.connectMCP(), m.chat.resumeActiveGoalCmd())
	case forkSessionResultMsg:
		m.current = m.chat
		return m, uikit.Batch(m.chat.applyForkResult(v), m.mouseModeCmd())
	}

	// Chat work is long-lived and independent of the visible screen. The root
	// commands form a chain (each stream chunk schedules the next pump), so
	// dropping even one runtime message while a settings screen is active stops
	// the turn permanently. Route those messages directly to the persistent chat
	// model while leaving the current screen untouched.
	if !m.chatVisible() && chatRuntimeMsg(msg) {
		_, cmd := m.chat.Update(msg)
		return m, cmd
	}
	next, cmd := m.current.Update(msg)
	m.current = next
	return m, cmd
}

func (m RootModel) View() string {
	if m.current == nil {
		return ""
	}
	return m.current.View()
}
