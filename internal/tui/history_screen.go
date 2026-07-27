package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/session"
)

// resumeSessionMsg pide al router cargar una conversación guardada.
type resumeSessionMsg struct{ sess *session.Session }

// HistoryModel implementa la pantalla `/history`: lista de conversaciones
// guardadas del proyecto actual, con búsqueda, reanudación y borrado.
type HistoryModel struct {
	ctx      *AppContext
	store    *session.Store
	project  string
	filter   textinput.Model
	all      []session.Meta
	filtered []session.Meta
	cursor   int
	err      string
}

func NewHistory(ctx *AppContext, store *session.Store, project string) *HistoryModel {
	ti := textinput.New()
	ti.Prompt = "  "
	ti.Placeholder = "Buscar conversación…"
	ti.Focus()
	ti.CharLimit = 64
	m := &HistoryModel{ctx: ctx, store: store, project: project, filter: ti}
	m.reload()
	return m
}

func (m *HistoryModel) reload() {
	metas, err := m.store.List(m.project)
	if err != nil {
		m.err = err.Error()
	}
	m.all = metas
	m.applyFilter()
}

func (m *HistoryModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		m.filtered = m.all
	} else {
		out := []session.Meta{}
		for _, r := range m.all {
			if strings.Contains(strings.ToLower(r.Title), q) || strings.Contains(strings.ToLower(r.ID), q) {
				out = append(out, r)
			}
		}
		m.filtered = out
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *HistoryModel) Init() tea.Cmd { return textinput.Blink }

func (m *HistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m, switchToChat()
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		case "ctrl+d":
			if len(m.filtered) == 0 {
				return m, nil
			}
			if err := m.store.Delete(m.project, m.filtered[m.cursor].ID); err != nil {
				m.err = err.Error()
			}
			m.reload()
			return m, nil
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			sess, err := m.store.Load(m.project, m.filtered[m.cursor].ID)
			if err != nil {
				return m, showError(err)
			}
			return m, func() tea.Msg { return resumeSessionMsg{sess: sess} }
		}
	}
	var cmd tea.Cmd
	prev := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != prev {
		m.cursor = 0
		m.applyFilter()
	}
	return m, cmd
}

func (m *HistoryModel) View() string {
	s := m.ctx.Styles
	w := min(m.ctx.Width-4, 90)
	if w < 30 {
		w = 30
	}
	header := s.Accent.Render("Historial de conversaciones") + "  " + s.Muted.Render("· "+m.project)
	filterBox := s.InputBoxFocused.Width(w).Render("🔍 " + m.filter.View())

	maxVisible := m.ctx.Height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	rows := []string{}
	for i := start; i < end; i++ {
		r := m.filtered[i]
		meta := s.Muted.Render(humanAgo(r.UpdatedAt) + "  ·  " + fmtInt(r.Turns) + " turnos")
		title := lipgloss.NewStyle().Foreground(s.Theme.Foreground).Render(clip(r.Title, w-28))
		line := "  " + title + "   " + meta
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Background(s.Theme.SurfaceHover).
				Foreground(s.Theme.Foreground).
				Width(w).
				Render("❯ " + clip(r.Title, w-28) + "   " + humanAgo(r.UpdatedAt))
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		rows = append(rows, s.Muted.Render("  No hay conversaciones guardadas en este proyecto."))
	}

	footer := s.Muted.Render("↑↓ Navegar   Enter Reanudar   Ctrl+D Borrar   Esc Cancelar")
	body := []string{header, "", filterBox, "", strings.Join(rows, "\n"), "", footer}
	if m.err != "" {
		body = append(body, s.Danger.Render(m.err))
	}
	return strings.Join(body, "\n")
}

// humanAgo formatea una marca de tiempo de forma compacta y en español.
func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "hace un momento"
	case d < time.Hour:
		return "hace " + fmtInt(int(d.Minutes())) + " min"
	case d < 24*time.Hour:
		return "hace " + fmtInt(int(d.Hours())) + " h"
	case d < 7*24*time.Hour:
		return "hace " + fmtInt(int(d.Hours()/24)) + " d"
	}
	return t.Format("02/01/2006 15:04")
}

var _ tea.Model = (*HistoryModel)(nil)
