package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/providers"
)

// modelRow is one selectable model. Provider information stays on the card so
// the list no longer needs separate provider header rows.
type modelRow struct {
	providerID string
	provName   string
	modelID    string
	label      string
	desc       string
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
	ti.Prompt = ""
	ti.Placeholder = "Escribe para filtrar modelos"
	ti.Focus()
	ti.CharLimit = 64
	if ctx.Width > 12 {
		ti.Width = ctx.Width - 12
	}
	m := ModelSelectorModel{ctx: ctx, filter: ti}
	m.rebuild()
	active := ctx.Providers.Active()
	for i, r := range m.filtered {
		if r.providerID == active.ProviderID && r.modelID == active.ModelID {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *ModelSelectorModel) rebuild() {
	rows := []modelRow{}
	for _, p := range m.ctx.Providers.Providers {
		for _, mod := range p.Models {
			context := "sin contexto definido"
			if mod.MaxContextTokens > 0 {
				context = humanTokens(mod.MaxContextTokens)
			}
			rows = append(rows, modelRow{
				providerID: p.ID,
				provName:   p.Name,
				modelID:    mod.ID,
				label:      mod.ID,
				desc:       context,
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
		for _, r := range m.all {
			_, modelMatch := subsequenceMatch(strings.ToLower(r.label), q)
			_, providerMatch := subsequenceMatch(strings.ToLower(r.provName), q)
			if modelMatch || providerMatch {
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

func (m ModelSelectorModel) Init() tea.Cmd { return textinput.Blink }

func (m ModelSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, switchToChat()
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			row := m.filtered[m.cursor]
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
		m.cursor = 0
		m.applyFilter()
	}
	return m, cmd
}

func (m ModelSelectorModel) View() string {
	filter := m.filter
	if w := viewportSelectorWidth(m.ctx.Width) - 10; w > 4 {
		filter.Width = w
	}
	active := m.ctx.Providers.Active()
	items := make([]viewportSelectorItem, 0, len(m.filtered))
	for _, row := range m.filtered {
		items = append(items, viewportSelectorItem{
			Primary: row.provName + " · " + row.label + " · " + row.desc,
			Active:  row.providerID == active.ProviderID && row.modelID == active.ModelID,
		})
	}
	return renderViewportSelector(m.ctx.Styles, viewportSelectorSpec{
		Title:         "Selecciona un modelo",
		Subtitle:      "Proveedor · modelo · contexto",
		SearchContent: "Buscar  " + filter.View(),
		Items:         items,
		Selected:      m.cursor,
		EmptyText:     "Sin resultados.",
		Footer:        "↑↓ navegar · Enter elegir · Esc cancelar",
		ScreenWidth:   m.ctx.Width,
		ScreenHeight:  m.ctx.Height,
	})
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

// subsequenceMatch returns true if all chars of needle appear in order.
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
