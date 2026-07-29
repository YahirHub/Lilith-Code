package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

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

type switchScreenMsg struct{ next tea.Model }
type systemMsg struct{ text string }
type errMsg struct{ err error }

func switchTo(m tea.Model) tea.Cmd { return func() tea.Msg { return switchScreenMsg{next: m} } }
func switchToChat() tea.Cmd        { return func() tea.Msg { return switchScreenMsg{next: nil} } } // nil = go to chat
func switchToChatWithSystem(s string) tea.Cmd {
	return tea.Batch(switchToChat(), func() tea.Msg { return systemMsg{text: s} })
}
func showError(err error) tea.Cmd { return func() tea.Msg { return errMsg{err: err} } }

// RootModel wraps whichever screen is active.
type RootModel struct {
	ctx     *AppContext
	current tea.Model
	chat    *ChatModel // persistent chat model
	cancel  context.CancelFunc
}

// chatRuntimeMsg reports whether msg belongs to the persistent chat runtime
// rather than to whichever screen happens to be visible. Provider chunks,
// tool results and animation/persistence ticks must keep reaching ChatModel
// while /config, /models, /providers, etc. are open; otherwise the command
// chain that pumps the stream is lost and the turn appears frozen on return.
func chatRuntimeMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case thinkingTickMsg,
		transcriptRefreshTickMsg,
		livePersistDoneMsg,
		livePersistTickMsg,
		cmdElapsedTickMsg,
		chatStreamMsg,
		toolResultsMsg,
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

// chatMouseModeCmd enables mouse reporting only when chat currently exposes a
// clickable inline control. Most of the time reporting remains disabled so the
// terminal keeps native text selection/copy behavior. A large TodoWrite also
// opts in because its visible block can be clicked to expand/collapse.
func (m *ChatModel) chatMouseModeCmd() tea.Cmd {
	if m != nil && (m.hasPendingPlanQuestions() || m.todoExpandable()) {
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

// mouseModeCmd keeps click support on settings/select screens but releases
// mouse capture in ordinary chat.
func (m RootModel) mouseModeCmd() tea.Cmd {
	if m.chatVisible() {
		if m.chat != nil {
			return m.chat.chatMouseModeCmd()
		}
		return tea.DisableMouse
	}
	return tea.EnableMouseCellMotion
}

// NewRootModel builds the root Bubble Tea model. If firstRun is true, the
// onboarding screen is shown; otherwise the chat opens directly.
func NewRootModel(ctx *AppContext) RootModel {
	chat := NewChat(ctx)
	m := RootModel{ctx: ctx, chat: &chat}
	if ctx.Resume != nil {
		chat.LoadSession(ctx.Resume)
	}
	if ctx.FirstRun {
		m.current = NewOnboarding(ctx, true)
	} else {
		m.current = &chat
	}
	return m
}

func (m RootModel) Init() tea.Cmd {
	if m.current == nil {
		return nil
	}
	// Program starts with WithMouseCellMotion so first-run/settings screens are
	// interactive from frame zero. Only chat needs an Init-time override.
	if m.chatVisible() {
		return tea.Batch(m.current.Init(), m.mouseModeCmd())
	}
	return m.current.Init()
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
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
			return m, m.mouseModeCmd()
		}
		m.current = v.next
		return m, tea.Batch(m.current.Init(), m.mouseModeCmd())
	case systemMsg:
		m.chat.AddSystem(v.text)
		return m, nil
	case errMsg:
		m.chat.AddError(v.err.Error())
		return m, nil
	case resumeSessionMsg:
		m.chat.LoadSession(v.sess)
		m.current = m.chat
		return m, m.mouseModeCmd()
	}

	// Chat work is long-lived and independent of the visible screen. Bubble Tea
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
