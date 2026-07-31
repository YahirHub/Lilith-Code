package tui

import (
	"fmt"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"

	"github.com/lilith/li/internal/providers"
)

// ProviderScreen is the interactive `/providers` manager. It deliberately
// keeps credential editing in `/login`; this screen owns selection and safe
// runtime preferences only.
type ProviderScreen struct {
	ctx           *AppContext
	selected      int
	focus         string
	message       string
	danger        string
	confirmDelete bool
}

func NewProviderScreen(ctx *AppContext) *ProviderScreen {
	m := &ProviderScreen{ctx: ctx, focus: "providers"}
	active := ctx.Providers.ActiveProviderID
	for i := range ctx.Providers.Providers {
		if ctx.Providers.Providers[i].ID == active {
			m.selected = i
			break
		}
	}
	return m
}

func (m *ProviderScreen) Init() uikit.Cmd { return nil }

func (m *ProviderScreen) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case uikit.MouseMsg:
		e := uikit.MouseEvent(v)
		if e.IsWheel() {
			switch e.Button {
			case uikit.MouseButtonWheelUp:
				if m.selected > 0 {
					m.selected--
				}
			case uikit.MouseButtonWheelDown:
				if m.selected < len(m.ctx.Providers.Providers)-1 {
					m.selected++
				}
			}
			m.focus = "providers"
			m.confirmDelete = false
			return m, nil
		}
		e, ok := mouseLeftPress(v)
		if !ok {
			return m, nil
		}
		_, hits := m.layout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return m, nil
		}
		return m.handleHit(hit)

	case uikit.KeyMsg:
		key := v.String()
		if m.confirmDelete && key == "esc" {
			m.confirmDelete = false
			m.message = "Eliminación cancelada."
			return m, nil
		}
		switch key {
		case "esc", "q":
			return m, switchToChat()
		case "tab":
			m.moveFocus(1)
			return m, nil
		case "shift+tab":
			m.moveFocus(-1)
			return m, nil
		case "up", "k":
			if m.focus == "providers" && m.selected > 0 {
				m.selected--
				m.confirmDelete = false
			}
			return m, nil
		case "down", "j":
			if m.focus == "providers" && m.selected < len(m.ctx.Providers.Providers)-1 {
				m.selected++
				m.confirmDelete = false
			}
			return m, nil
		case "left", "right", " ":
			if m.focus == "streaming" {
				return m.toggleStreaming()
			}
		case "enter":
			return m.activateFocus()
		case "m":
			return m, switchTo(NewModelSelector(m.ctx))
		case "a":
			return m, switchTo(NewOnboarding(m.ctx, false))
		case "d":
			if p := m.selectedProvider(); p != nil && !p.Bundled {
				return m.deleteSelected()
			}
		}
	}
	return m, nil
}

func (m *ProviderScreen) focusOrder() []string {
	order := []string{"providers"}
	if p := m.selectedProvider(); p != nil && !p.Bundled {
		order = append(order, "streaming")
	}
	order = append(order, "activate", "models", "add")
	if p := m.selectedProvider(); p != nil && !p.Bundled {
		order = append(order, "delete")
	}
	order = append(order, "back")
	return order
}

func (m *ProviderScreen) moveFocus(delta int) {
	order := m.focusOrder()
	idx := 0
	for i, id := range order {
		if id == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(order)) % len(order)
	m.focus = order[idx]
}

func (m *ProviderScreen) activateFocus() (uikit.Model, uikit.Cmd) {
	switch m.focus {
	case "providers", "activate":
		return m.activateSelected()
	case "streaming":
		return m.toggleStreaming()
	case "models":
		return m, switchTo(NewModelSelector(m.ctx))
	case "add":
		return m, switchTo(NewOnboarding(m.ctx, false))
	case "delete":
		return m.deleteSelected()
	case "back":
		return m, switchToChat()
	}
	return m, nil
}

func (m *ProviderScreen) handleHit(hit settingsHit) (uikit.Model, uikit.Cmd) {
	if strings.HasPrefix(hit.id, "provider:") {
		id := strings.TrimPrefix(hit.id, "provider:")
		for i := range m.ctx.Providers.Providers {
			if m.ctx.Providers.Providers[i].ID == id {
				m.selected = i
				m.focus = "providers"
				m.confirmDelete = false
				return m, nil
			}
		}
		return m, nil
	}
	m.focus = hit.id
	switch hit.id {
	case "streaming":
		return m.toggleStreaming()
	case "activate":
		return m.activateSelected()
	case "models":
		return m, switchTo(NewModelSelector(m.ctx))
	case "add":
		return m, switchTo(NewOnboarding(m.ctx, false))
	case "delete":
		return m.deleteSelected()
	case "back":
		return m, switchToChat()
	}
	return m, nil
}

func (m *ProviderScreen) selectedProvider() *providers.Provider {
	if len(m.ctx.Providers.Providers) == 0 {
		return nil
	}
	m.selected = clampInt(m.selected, 0, len(m.ctx.Providers.Providers)-1)
	return &m.ctx.Providers.Providers[m.selected]
}

func (m *ProviderScreen) activateSelected() (uikit.Model, uikit.Cmd) {
	p := m.selectedProvider()
	if p == nil || len(p.Models) == 0 {
		m.danger = "El proveedor no tiene modelos configurados."
		return m, nil
	}
	modelID := p.Models[0].ID
	active := m.ctx.Providers.Active()
	if active.ProviderID == p.ID && active.ModelID != "" {
		modelID = active.ModelID
	}
	if err := providers.SetActive(m.ctx.ConfigDir, p.ID, modelID); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	if err := m.ctx.ReloadProviders(); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	m.danger = ""
	m.message = "Proveedor activo: " + p.Name + " / " + modelID
	return m, nil
}

func (m *ProviderScreen) toggleStreaming() (uikit.Model, uikit.Cmd) {
	p := m.selectedProvider()
	if p == nil || p.Bundled {
		return m, nil
	}
	if err := providers.SetUseNonStreaming(m.ctx.ConfigDir, p.ID, !p.UseNonStreaming); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	if err := m.ctx.ReloadProviders(); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	m.danger = ""
	if current := m.selectedProvider(); current != nil && current.UseNonStreaming {
		m.message = "Streaming desactivado para " + current.Name + "."
	} else if current != nil {
		m.message = "Streaming activado para " + current.Name + "."
	}
	return m, nil
}

func (m *ProviderScreen) deleteSelected() (uikit.Model, uikit.Cmd) {
	p := m.selectedProvider()
	if p == nil || p.Bundled {
		return m, nil
	}
	if !m.confirmDelete {
		m.confirmDelete = true
		m.message = "Pulsa Eliminar otra vez para borrar " + p.Name + "."
		return m, nil
	}
	id, name := p.ID, p.Name
	if err := providers.Delete(m.ctx.ConfigDir, id); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	if err := m.ctx.ReloadProviders(); err != nil {
		m.danger = err.Error()
		return m, nil
	}
	if m.ctx.Providers.ActiveProviderID == "" {
		for _, candidate := range m.ctx.Providers.Providers {
			if len(candidate.Models) == 0 {
				continue
			}
			if err := providers.SetActive(m.ctx.ConfigDir, candidate.ID, candidate.Models[0].ID); err == nil {
				_ = m.ctx.ReloadProviders()
				break
			}
		}
	}
	if m.selected >= len(m.ctx.Providers.Providers) {
		m.selected = len(m.ctx.Providers.Providers) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.confirmDelete = false
	m.danger = ""
	m.message = "Proveedor eliminado: " + name
	m.focus = "providers"
	return m, nil
}

func (m *ProviderScreen) providerCard(index, width int) settingsBlock {
	p := m.ctx.Providers.Providers[index]
	meta := fmt.Sprintf("%d modelos · %s", len(p.Models), authLabel(p.Auth))
	if p.BaseURL != "" {
		meta += " · " + p.BaseURL
	}
	badge := "OPENAI"
	if p.Bundled {
		badge = "INCLUIDO"
	}
	return settingsCard(m.ctx.Styles, settingsCardSpec{
		ID:          "provider:" + p.ID,
		Title:       p.Name,
		Description: providerDescription(p, m.ctx.Providers),
		Meta:        meta,
		Badge:       badge,
		Selected:    index == m.selected,
		Active:      p.ID == m.ctx.Providers.ActiveProviderID,
		Width:       width,
	})
}

func providerVisibleRange(cards []settingsBlock, selected, budget int) (int, int) {
	if len(cards) == 0 {
		return 0, 0
	}
	selected = clampInt(selected, 0, len(cards)-1)
	if budget < 1 {
		budget = 1
	}
	// Start with the selected card and fill backwards first. This guarantees
	// that keyboard navigation never moves selection outside the visible area.
	start := selected
	used := tuistyle.Height(cards[selected].text) + 1
	for start > 0 {
		h := tuistyle.Height(cards[start-1].text) + 1
		if used+h > budget {
			break
		}
		start--
		used += h
	}
	end := selected + 1
	for end < len(cards) {
		h := tuistyle.Height(cards[end].text) + 1
		if used+h > budget {
			break
		}
		end++
		used += h
	}
	return start, end
}

func (m *ProviderScreen) layout() (string, []settingsHit) {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	c := newSettingsCanvas(w)
	c.block(settingsHeader(s, "Proveedores", "Selecciona, activa y administra conexiones. Ratón y teclado funcionan en paralelo."))
	c.blank()

	p := m.selectedProvider()
	var streamBlock settingsBlock
	if p != nil {
		streamDesc := "Usa respuestas SSE progresivas cuando el endpoint las soporta."
		disabled := p.Bundled
		if disabled {
			streamDesc = "Este proveedor incluido administra su modo de transporte internamente."
		}
		streamBlock = settingsSwitch(s, settingsSwitchSpec{
			ID:          "streaming",
			Label:       "Streaming",
			Description: streamDesc,
			Value:       !p.UseNonStreaming,
			Disabled:    disabled,
			Focused:     m.focus == "streaming",
			Width:       w,
		})
	}

	deleteLabel := "Eliminar"
	if m.confirmDelete {
		deleteLabel = "Confirmar eliminar"
	}
	buttons := []settingsButtonSpec{
		{ID: "activate", Label: "Activar", Focused: m.focus == "activate", Disabled: p == nil},
		{ID: "models", Label: "Modelos", Focused: m.focus == "models", Disabled: p == nil},
		{ID: "add", Label: "Agregar proveedor", Focused: m.focus == "add"},
	}
	if p != nil && !p.Bundled {
		buttons = append(buttons, settingsButtonSpec{ID: "delete", Label: deleteLabel, Danger: true, Focused: m.focus == "delete"})
	}
	buttons = append(buttons, settingsButtonSpec{ID: "back", Label: "Volver", Focused: m.focus == "back"})
	buttonBlock := settingsButtonGroup(s, w, buttons...)

	providersList := m.ctx.Providers.Providers
	if len(providersList) == 0 {
		c.line(s.Muted.Render("No hay proveedores configurados."))
		c.blank()
	} else {
		cards := make([]settingsBlock, len(providersList))
		for i := range providersList {
			cards[i] = m.providerCard(i, w)
		}
		// Reserve rows for the controls below the catalog. Unlike a fixed card
		// count, this remains usable when wrapped URLs make a card taller.
		reserved := 10 + tuistyle.Height(buttonBlock.text)
		if streamBlock.text != "" {
			reserved += tuistyle.Height(streamBlock.text) + 1
		}
		if m.message != "" {
			reserved += tuistyle.Height(settingsWrapPlain(m.message, w)) + 1
		}
		if m.danger != "" {
			reserved += tuistyle.Height(settingsWrapPlain(m.danger, w)) + 1
		}
		budget := m.ctx.Height - reserved
		start, end := providerVisibleRange(cards, m.selected, budget)
		for i := start; i < end; i++ {
			c.block(cards[i])
			c.blank()
		}
		if len(providersList) > end-start {
			c.line(s.Muted.Render(fmt.Sprintf("Mostrando %d-%d de %d · ↑↓ para recorrer", start+1, end, len(providersList))))
			c.blank()
		}
	}

	if streamBlock.text != "" {
		c.block(streamBlock)
		c.blank()
	}
	c.block(buttonBlock)
	if m.message != "" {
		c.blank()
		c.line(s.Success.Render("· " + settingsWrapPlain(m.message, w)))
	}
	if m.danger != "" {
		c.blank()
		c.line(s.Danger.Render("✗ " + settingsWrapPlain(m.danger, w)))
	}
	c.blank()
	c.block(settingsFooter(s, "↑↓ proveedor · Tab controles · Enter activar/usar · Espacio switch · clic izquierdo · Esc volver"))
	return c.render(m.ctx.Width)
}

func (m *ProviderScreen) View() string {
	view, _ := m.layout()
	return view
}

func providerDescription(p providers.Provider, cfg providers.Config) string {
	active := cfg.Active()
	if active.ProviderID == p.ID && active.ModelID != "" {
		return "Modelo activo: " + active.ModelID
	}
	if len(p.Models) > 0 {
		return "Modelo inicial: " + p.Models[0].ID
	}
	return "Sin modelos configurados"
}

func authLabel(kind providers.AuthKind) string {
	switch kind {
	case providers.AuthNone:
		return "sin autenticación"
	case providers.AuthAPIKey:
		return "API key"
	case providers.AuthEnv:
		return "variable de entorno"
	case providers.AuthOAuth:
		return "OAuth"
	case providers.AuthBundled:
		return "incluido"
	default:
		return string(kind)
	}
}

var _ uikit.Model = (*ProviderScreen)(nil)
