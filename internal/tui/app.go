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
	if m.current != nil {
		return m.current.Init()
	}
	return nil
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
			return m, nil
		}
		m.current = v.next
		return m, m.current.Init()
	case systemMsg:
		m.chat.AddSystem(v.text)
		return m, nil
	case errMsg:
		m.chat.AddError(v.err.Error())
		return m, nil
	case resumeSessionMsg:
		m.chat.LoadSession(v.sess)
		m.current = m.chat
		return m, nil
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
