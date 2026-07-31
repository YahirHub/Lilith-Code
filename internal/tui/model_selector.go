package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/tui/uikit"
	"github.com/lilith/li/internal/tui/uikit/textinput"
)

const modelCatalogRefreshTimeout = 15 * time.Second

// modelRow is one selectable model. Provider information stays on the card so
// the list no longer needs separate provider header rows.
type modelRow struct {
	providerID string
	provName   string
	modelID    string
	label      string
	desc       string
}

type modelCatalogRefreshedMsg struct {
	config providers.Config
	report providers.RefreshReport
}

// ModelSelectorModel implements the /models screen.
type ModelSelectorModel struct {
	ctx            *AppContext
	filter         textinput.Model
	all            []modelRow
	filtered       []modelRow
	cursor         int
	refreshing     bool
	refreshMessage string
	loadError      string
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
	m := ModelSelectorModel{ctx: ctx, filter: ti, refreshing: true}
	m.rebuild()
	m.selectActive()
	return m
}

func refreshModelCatalogCmd(ctx *AppContext) uikit.Cmd {
	if ctx == nil {
		return nil
	}
	cfg := ctx.Providers
	dir := ctx.ConfigDir
	return func() uikit.Msg {
		refreshCtx, cancel := context.WithTimeout(context.Background(), modelCatalogRefreshTimeout)
		defer cancel()
		updated, report := providers.RefreshConnectedModels(refreshCtx, dir, cfg, providers.RefreshOptions{})
		return modelCatalogRefreshedMsg{config: updated, report: report}
	}
}

func (m *ModelSelectorModel) rebuild() {
	connected, err := providers.ConnectedProviders(m.ctx.ConfigDir, m.ctx.Providers)
	if err != nil {
		m.all = nil
		m.filtered = nil
		m.loadError = err.Error()
		return
	}
	m.loadError = ""
	rows := []modelRow{}
	for _, p := range connected {
		for _, mod := range p.Models {
			contextWindow := "sin contexto definido"
			if mod.MaxContextTokens > 0 {
				contextWindow = humanTokens(mod.MaxContextTokens)
			}
			rows = append(rows, modelRow{
				providerID: p.ID,
				provName:   p.Name,
				modelID:    mod.ID,
				label:      mod.ID,
				desc:       contextWindow,
			})
		}
	}
	m.all = rows
	m.applyFilter()
}

func (m *ModelSelectorModel) selectActive() {
	active := m.ctx.Providers.Active()
	for i, row := range m.filtered {
		if row.providerID == active.ProviderID && row.modelID == active.ModelID {
			m.cursor = i
			return
		}
	}
	if len(m.filtered) > 0 {
		m.cursor = clampInt(m.cursor, 0, len(m.filtered)-1)
	} else {
		m.cursor = 0
	}
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

func (m ModelSelectorModel) Init() uikit.Cmd {
	return uikit.Batch(textinput.Blink, refreshModelCatalogCmd(m.ctx))
}

func (m ModelSelectorModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch msg := msg.(type) {
	case modelCatalogRefreshedMsg:
		m.ctx.Providers = msg.config
		m.refreshing = false
		m.refreshMessage = summarizeCatalogRefresh(msg.report)
		m.rebuild()
		m.selectActive()
		return m, nil
	case uikit.KeyMsg:
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
		case "ctrl+r":
			if m.refreshing {
				return m, nil
			}
			m.refreshing = true
			m.refreshMessage = ""
			return m, refreshModelCatalogCmd(m.ctx)
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			row := m.filtered[m.cursor]
			if err := providers.SetActive(m.ctx.ConfigDir, row.providerID, row.modelID); err != nil {
				return m, showError(err)
			}
			if err := m.ctx.ReloadProviders(); err != nil {
				return m, showError(err)
			}
			return m, switchToChatWithSystem("Modelo activo: " + row.provName + " / " + row.modelID)
		}
	}
	var cmd uikit.Cmd
	prev := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != prev {
		m.cursor = 0
		m.applyFilter()
	}
	return m, cmd
}

func summarizeCatalogRefresh(report providers.RefreshReport) string {
	updated := report.UpdatedCount()
	errorsCount := len(report.Errors)
	manualCount := report.UnsupportedCount()
	switch {
	case updated > 0 && errorsCount > 0:
		return fmt.Sprintf("Catálogos actualizados: %d · %d proveedor(es) no respondieron", updated, errorsCount)
	case errorsCount > 0:
		return fmt.Sprintf("No se pudieron actualizar %d catálogo(s); se conserva la caché disponible", errorsCount)
	case updated > 0 && manualCount > 0:
		return fmt.Sprintf("Catálogos actualizados: %d · %d proveedor(es) usan modelos manuales", updated, manualCount)
	case updated > 0:
		return fmt.Sprintf("Catálogos actualizados: %d", updated)
	case manualCount > 0:
		return fmt.Sprintf("Catálogos al día · %d proveedor(es) usan modelos manuales", manualCount)
	default:
		return "Catálogos al día"
	}
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
	subtitle := "Sólo proveedores conectados · proveedor · modelo · contexto"
	if m.refreshing {
		subtitle = "Actualizando catálogos conectados…"
	} else if m.refreshMessage != "" {
		subtitle = m.refreshMessage
	}
	empty := "No hay modelos disponibles en proveedores conectados. Usa /login para conectar uno."
	if strings.TrimSpace(m.filter.Value()) != "" && len(m.all) > 0 {
		empty = "Sin resultados para este filtro."
	}
	if m.loadError != "" {
		empty = "No se pudo comprobar qué proveedores están conectados: " + m.loadError
	}
	return renderViewportSelector(m.ctx.Styles, viewportSelectorSpec{
		Title:         "Selecciona un modelo",
		Subtitle:      subtitle,
		SearchContent: "Buscar  " + filter.View(),
		Items:         items,
		Selected:      m.cursor,
		EmptyText:     empty,
		Footer:        "↑↓ navegar · Enter elegir · Ctrl+R actualizar · Esc cancelar",
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
