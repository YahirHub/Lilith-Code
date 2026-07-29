package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lilith/li/internal/websearch"
)

func (s *searchConfigState) appendLayout(canvas *settingsCanvas, width int) {
	s.reload()
	switch s.view {
	case searchViewKey:
		s.appendKeyLayout(canvas, width)
	case searchViewOrder:
		s.appendOrderLayout(canvas, width)
	default:
		s.appendMainLayout(canvas, width)
	}
}

func (s *searchConfigState) appendMainLayout(canvas *settingsCanvas, width int) {
	styles := s.ctx.Styles
	available := websearch.AvailableOrder(s.settings, s.auth)
	status := "INACTIVO"
	if len(available) > 0 {
		status = "ACTIVO"
	}
	defaultLabel := "ninguno"
	if s.settings.DefaultProvider != "" && websearch.Resolve(s.settings.DefaultProvider, s.settings, s.auth).Available {
		defaultLabel = websearch.Labels[s.settings.DefaultProvider]
	}
	canvas.block(settingsCard(styles, settingsCardSpec{
		Title:       "Búsqueda web · " + status,
		Description: fmt.Sprintf("%d motor(es) validados · predeterminado: %s", len(available), defaultLabel),
		Meta:        "La herramienta web_search sólo existe para el agente cuando hay al menos un motor validado y habilitado.",
		Badge:       status,
		Width:       width,
	}))
	canvas.blank()

	providerButtons := make([]settingsButtonSpec, 0, len(websearch.ProviderIDs))
	for _, provider := range websearch.ProviderIDs {
		id := "search-provider:" + string(provider)
		providerButtons = append(providerButtons, settingsButtonSpec{
			ID:      id,
			Label:   websearch.Labels[provider] + " " + s.stateLabel(provider),
			Focused: s.focus == id,
		})
	}
	canvas.block(settingsButtonGroup(styles, width, providerButtons...))
	canvas.blank()

	provider := s.selected
	state := websearch.Resolve(provider, s.settings, s.auth)
	meta := "Última prueba: sin ejecutar"
	if state.LastTest != nil {
		result := "ERROR"
		if state.LastTest.OK {
			result = "OK"
		}
		meta = fmt.Sprintf("Última prueba: %s · %s", result, state.LastTest.Message)
	}
	canvas.block(settingsCard(styles, settingsCardSpec{
		Title:       websearch.Labels[provider],
		Description: "API key: " + websearch.MaskAPIKey(state.APIKey),
		Meta:        meta,
		Badge:       s.stateLabel(provider),
		Active:      provider == s.settings.DefaultProvider && state.Available,
		Selected:    strings.HasPrefix(s.focus, "search-provider:") && s.focus == "search-provider:"+string(provider),
		Width:       width,
	}))
	canvas.blank()

	keyLabel := "Configurar API key"
	if state.Configured {
		keyLabel = "Reemplazar API key"
	}
	toggleLabel := "Habilitar"
	if state.Configured && state.EnabledByUser {
		toggleLabel = "Deshabilitar"
	}
	canvas.block(settingsButtonGroup(styles, width,
		settingsButtonSpec{ID: "search-key", Label: keyLabel, Focused: s.focus == "search-key", Disabled: s.busy},
		settingsButtonSpec{ID: "search-test", Label: "Probar conexión", Focused: s.focus == "search-test", Disabled: s.busy || !state.Configured},
		settingsButtonSpec{ID: "search-toggle", Label: toggleLabel, Focused: s.focus == "search-toggle", Disabled: s.busy || !state.Configured || (!state.Validated && !state.EnabledByUser)},
		settingsButtonSpec{ID: "search-default", Label: "Usar primero", Focused: s.focus == "search-default", Disabled: s.busy || !state.Available || provider == s.settings.DefaultProvider},
		settingsButtonSpec{ID: "search-remove", Label: "Eliminar API key", Danger: true, Focused: s.focus == "search-remove", Disabled: s.busy || !state.Configured},
	))
	canvas.blank()
	canvas.block(settingsButtonGroup(styles, width,
		settingsButtonSpec{ID: "search-order", Label: "Ordenar respaldos", Focused: s.focus == "search-order", Disabled: s.busy || len(available) < 2},
		settingsButtonSpec{ID: "search-test-all", Label: "Probar configurados", Focused: s.focus == "search-test-all", Disabled: s.busy || !s.hasConfigured()},
		settingsButtonSpec{ID: "back", Label: "Volver al chat", Focused: s.focus == "back"},
	))
	if s.busy {
		canvas.blank()
		canvas.line(styles.Accent.Render("Probando conexión..."))
	}
	s.appendMessages(canvas, width)
	canvas.blank()
	canvas.block(settingsFooter(styles, "Tab/Shift+Tab cambiar sección · ↑↓ mover foco · Enter usar · clic · Esc volver"))
}

func (s *searchConfigState) appendKeyLayout(canvas *settingsCanvas, width int) {
	styles := s.ctx.Styles
	provider := s.selected
	canvas.block(settingsCard(styles, settingsCardSpec{
		Title:       "API key de " + websearch.Labels[provider],
		Description: "Se guardará en " + filepath.Join(s.ctx.ConfigDir, websearch.AuthFile) + " separada de la configuración general.",
		Meta:        "Al guardar se hace una búsqueda real. El motor no estará disponible para el agente si la prueba falla.",
		Badge:       "SECRETO",
		Width:       width,
	}))
	canvas.blank()
	inputWidth := width - 6
	if inputWidth < 8 {
		inputWidth = 8
	}
	s.input.Width = inputWidth
	canvas.block(settingsInput(styles, settingsInputSpec{
		ID:       "search-key-input",
		Content:  s.input.View(),
		Width:    width,
		Focused:  !s.busy,
		Disabled: s.busy,
	}))
	canvas.blank()
	canvas.block(settingsButtonGroup(styles, width,
		settingsButtonSpec{ID: "search-key-save", Label: "Guardar y probar", Focused: true, Disabled: s.busy},
		settingsButtonSpec{ID: "search-key-cancel", Label: "Cancelar", Disabled: s.busy},
	))
	if s.busy {
		canvas.blank()
		canvas.line(styles.Accent.Render("Validando " + websearch.Labels[provider] + "..."))
	}
	s.appendMessages(canvas, width)
	canvas.blank()
	canvas.block(settingsFooter(styles, "Enter guardar y probar · Esc cancelar"))
}

func (s *searchConfigState) appendOrderLayout(canvas *settingsCanvas, width int) {
	styles := s.ctx.Styles
	defaultLabel := "ninguno"
	if s.settings.DefaultProvider != "" {
		defaultLabel = websearch.Labels[s.settings.DefaultProvider]
	}
	canvas.block(settingsCard(styles, settingsCardSpec{
		Title:       "Orden de respaldo",
		Description: "Predeterminado fijo: " + defaultLabel,
		Meta:        "Si el primer motor falla, tiene cuota agotada o no devuelve resultados útiles, Lilith intenta el siguiente.",
		Width:       width,
	}))
	canvas.blank()
	for i, provider := range s.order {
		canvas.block(settingsCard(styles, settingsCardSpec{
			ID:         "search-order:" + string(provider),
			Title:      fmt.Sprintf("%d. %s", i+1, websearch.Labels[provider]),
			Badge:      "RESPALDO",
			Selected:   i == s.orderAt,
			SingleLine: true,
			Width:      width,
		}))
		if i+1 < len(s.order) {
			canvas.blank()
		}
	}
	canvas.blank()
	canvas.block(settingsButtonGroup(styles, width,
		settingsButtonSpec{ID: "search-order-save", Label: "Guardar orden", Focused: true},
		settingsButtonSpec{ID: "search-order-back", Label: "Volver"},
	))
	s.appendMessages(canvas, width)
	canvas.blank()
	canvas.block(settingsFooter(styles, "↑↓ seleccionar · ←→ mover · Enter guardar · Esc volver"))
}

func (s *searchConfigState) appendMessages(canvas *settingsCanvas, width int) {
	styles := s.ctx.Styles
	if strings.TrimSpace(s.message) != "" {
		canvas.blank()
		for _, line := range strings.Split(s.message, "\n") {
			canvas.line(styles.Success.Render(settingsWrapPlain(line, width)))
		}
	}
	if strings.TrimSpace(s.danger) != "" {
		canvas.blank()
		for _, line := range strings.Split(s.danger, "\n") {
			canvas.line(styles.Danger.Render(settingsWrapPlain(line, width)))
		}
	}
}

func (s *searchConfigState) hasConfigured() bool {
	for _, provider := range websearch.ProviderIDs {
		if strings.TrimSpace(s.auth.APIKeys[provider]) != "" {
			return true
		}
	}
	return false
}
