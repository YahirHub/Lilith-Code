package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/providers"
)

// modelRow is one row of the selector list (either a provider header or a model).
type modelRow struct {
	isHeader   bool
	providerID string
	provName   string
	modelID    string
	label      string
	desc       string
	matchIdx   []int // subsequence match indices on label
}

// ModelSelectorModel implements the /models screen.
type ModelSelectorModel struct {
	ctx      *AppContext
	filter   textinput.Model
	all      []modelRow
	filtered []modelRow
	cursor   int
}

func NewModelSelector(ctx *AppContext) ModelSelectorModel {
	ti := textinput.New()
	ti.Prompt = "  "
	ti.Placeholder = "Buscar modelo…"
	ti.Focus()
	ti.CharLimit = 64
	m := ModelSelectorModel{ctx: ctx, filter: ti}
	m.rebuild()
	// Position cursor on the currently active model.
	active := ctx.Providers.Active()
	for i, r := range m.filtered {
		if !r.isHeader && r.providerID == active.ProviderID && r.modelID == active.ModelID {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *ModelSelectorModel) rebuild() {
	rows := []modelRow{}
	for _, p := range m.ctx.Providers.Providers {
		rows = append(rows, modelRow{isHeader: true, providerID: p.ID, provName: p.Name, label: p.Name})
		for _, mod := range p.Models {
			desc := p.Name
			if mod.MaxContextTokens > 0 {
				desc = p.Name + "  ·  " + humanTokens(mod.MaxContextTokens)
			}
			rows = append(rows, modelRow{
				providerID: p.ID,
				provName:   p.Name,
				modelID:    mod.ID,
				label:      mod.ID,
				desc:       desc,
			})
		}
	}
	m.all = rows
	m.applyFilter()
}

func (m *ModelSelectorModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		m.filtered = m.all
	} else {
		out := []modelRow{}
		var lastHeader *modelRow
		var headerAdded bool
		for i, r := range m.all {
			if r.isHeader {
				lastHeader = &m.all[i]
				headerAdded = false
				continue
			}
			indices, ok := subsequenceMatch(strings.ToLower(r.label), q)
			if !ok {
				continue
			}
			if !headerAdded && lastHeader != nil {
				out = append(out, *lastHeader)
				headerAdded = true
			}
			r.matchIdx = indices
			out = append(out, r)
		}
		m.filtered = out
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.snapCursorToModel(+1)
}

func (m *ModelSelectorModel) snapCursorToModel(dir int) {
	if len(m.filtered) == 0 {
		return
	}
	for i := 0; i < len(m.filtered); i++ {
		if !m.filtered[m.cursor].isHeader {
			return
		}
		m.cursor += dir
		if m.cursor < 0 {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor >= len(m.filtered) {
			m.cursor = 0
		}
	}
}

func (m ModelSelectorModel) Init() tea.Cmd { return textinput.Blink }

func (m ModelSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.snapCursorToModel(-1)
			return m, nil
		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			m.snapCursorToModel(+1)
			return m, nil
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			row := m.filtered[m.cursor]
			if row.isHeader {
				return m, nil
			}
			// La fila ya pertenece al catálogo cargado en memoria, así que no hace
			// falta volver a consultar proveedores remotos para validarla. Persistimos
			// sólo la selección y actualizamos el contexto compartido: el siguiente
			// turno toma este modelo sin reiniciar la CLI.
			cfg := m.ctx.Providers
			cfg.ActiveProviderID = row.providerID
			cfg.ActiveModelID = row.modelID
			if err := providers.Save(m.ctx.ConfigDir, cfg); err != nil {
				return m, showError(err)
			}
			m.ctx.Providers.ActiveProviderID = row.providerID
			m.ctx.Providers.ActiveModelID = row.modelID
			return m, switchToChatWithSystem("Modelo activo: " + row.provName + " / " + row.modelID)
		}
	}
	var cmd tea.Cmd
	prev := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != prev {
		m.applyFilter()
		m.cursor = 0
		m.snapCursorToModel(+1)
	}
	return m, cmd
}

func (m ModelSelectorModel) View() string {
	s := m.ctx.Styles
	w := min(m.ctx.Width-4, 90)
	header := s.Accent.Render("Selecciona un modelo") + "  " + s.Muted.Render("· escribe para filtrar")

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
		if r.isHeader {
			rows = append(rows, s.Subtitle.Bold(true).Render("▸ "+r.provName))
			continue
		}
		label := highlightMatches(r.label, r.matchIdx, s.Theme.Primary, s.Theme.Foreground)
		desc := s.Muted.Render(r.desc)
		line := "  " + label + "   " + desc
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Background(s.Theme.SurfaceHover).
				Foreground(s.Theme.Foreground).
				Width(w).
				Render("❯ " + label + "   " + desc)
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		rows = append(rows, s.Muted.Render("  Sin resultados."))
	}

	footer := s.Muted.Render("↑↓ Navegar   Enter Elegir   Esc Cancelar")

	body := strings.Join([]string{
		header,
		"",
		filterBox,
		"",
		strings.Join(rows, "\n"),
		"",
		footer,
	}, "\n")
	return body
}

func humanTokens(n int) string {
	if n >= 1_000_000 {
		return fmtDec(n, 1_000_000) + "M ctx"
	}
	if n >= 1000 {
		return fmtDec(n, 1000) + "K ctx"
	}
	return fmtInt(n) + " ctx"
}

func fmtDec(n, div int) string {
	whole := n / div
	rem := (n * 10 / div) % 10
	if rem == 0 {
		return fmtInt(whole)
	}
	return fmtInt(whole) + "." + string(rune('0'+rem))
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// subsequenceMatch returns matched indices if all chars of needle appear in order.
func subsequenceMatch(hay, needle string) ([]int, bool) {
	if needle == "" {
		return nil, true
	}
	idx := []int{}
	j := 0
	needleRunes := []rune(needle)
	for i, r := range hay {
		if j < len(needleRunes) && r == needleRunes[j] {
			idx = append(idx, i)
			j++
		}
	}
	if j == len(needleRunes) {
		return idx, true
	}
	return nil, false
}

func highlightMatches(text string, indices []int, hl, base lipgloss.Color) string {
	if len(indices) == 0 {
		return lipgloss.NewStyle().Foreground(base).Render(text)
	}
	set := map[int]bool{}
	for _, i := range indices {
		set[i] = true
	}
	baseStyle := lipgloss.NewStyle().Foreground(base)
	hlStyle := lipgloss.NewStyle().Foreground(hl).Bold(true)
	var b strings.Builder
	for i, r := range text {
		if set[i] {
			b.WriteString(hlStyle.Render(string(r)))
		} else {
			b.WriteString(baseStyle.Render(string(r)))
		}
	}
	return b.String()
}
