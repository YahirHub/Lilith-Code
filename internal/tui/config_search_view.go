package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"

	"github.com/lilith/li/internal/websearch"
)

func (s *searchConfigState) appendLayout(canvas *settingsCanvas, width int, contentFocused bool) {
	s.reload()
	switch s.view {
	case searchViewKey:
		s.appendKeyLayout(canvas, width)
	case searchViewOrder:
		s.appendOrderLayout(canvas, width)
	case searchViewProvider:
		s.appendProviderLayout(canvas, width)
	default:
		s.appendListLayout(canvas, width, contentFocused)
	}
}

func (s *searchConfigState) appendListLayout(canvas *settingsCanvas, width int, contentFocused bool) {
	styles := s.ctx.Styles
	available := websearch.AvailableOrder(s.settings, s.auth)
	configured := s.configuredCount()

	canvas.line(styles.Title.Render("Motores de búsqueda"))
	canvas.line(styles.Muted.Render(fmt.Sprintf("%d configurados · %d activos · Enter o clic para configurar", configured, len(available))))
	canvas.blank()
	canvas.block(s.providerListBlock(width, contentFocused))

	if s.busy {
		canvas.blank()
		canvas.line(styles.Accent.Render("Probando conexiones..."))
	}
	s.appendMessages(canvas, width)
	canvas.blank()
	if contentFocused {
		canvas.block(settingsFooter(styles, "↑↓ navegar · Enter configurar · ↑ desde el primero vuelve a secciones · Esc volver"))
	} else {
		canvas.block(settingsFooter(styles, "←→ cambiar sección · ↓ entrar a motores · clic para abrir · Esc volver"))
	}
}

func (s *searchConfigState) providerListBlock(width int, contentFocused bool) settingsBlock {
	styles := s.ctx.Styles
	if width < 12 {
		width = 12
	}
	lines := make([]string, 0, len(websearch.ProviderIDs)*2)
	hits := make([]settingsHit, 0, len(websearch.ProviderIDs))
	y := 0
	for _, provider := range websearch.ProviderIDs {
		state := websearch.Resolve(provider, s.settings, s.auth)
		selected := contentFocused && provider == s.selected
		prefix := "  "
		if selected {
			prefix = "> "
		}

		labelStyle := styles.Title
		if state.Configured {
			labelStyle = styles.Success.Bold(true)
		}
		status := s.providerListStatus(provider)
		statusRaw := "[" + status + "]"
		labelWidth := width - tuistyle.Width(prefix) - tuistyle.Width(statusRaw) - 2
		if labelWidth < 1 {
			labelWidth = 1
		}
		label := labelStyle.Render(settingsFitSingleLine(websearch.Labels[provider], labelWidth))
		statusStyled := s.providerStatusStyle(state).Render(statusRaw)
		gap := width - tuistyle.Width(prefix) - tuistyle.Width(label) - tuistyle.Width(statusStyled)
		if gap < 1 {
			gap = 1
		}
		first := prefix + label + strings.Repeat(" ", gap) + statusStyled

		metaWidth := width - 4
		if metaWidth < 1 {
			metaWidth = 1
		}
		second := "    " + settingsFitSingleLine(s.providerListMeta(provider), metaWidth)
		second = styles.Muted.Render(second)

		if selected {
			rowStyle := tuistyle.NewStyle().Width(width).Background(styles.Theme.Surface)
			first = rowStyle.Render(first)
			second = rowStyle.Render(second)
		}
		lines = append(lines, first, second)
		hits = append(hits, settingsHit{
			id:   "search-provider:" + string(provider),
			rect: settingsRect{x: 0, y: y, w: width, h: 2},
		})
		y += 2
	}
	return settingsBlock{text: strings.Join(lines, "\n"), hits: hits}
}

func (s *searchConfigState) providerStatusStyle(state websearch.State) tuistyle.Style {
	styles := s.ctx.Styles
	switch {
	case state.Available:
		return styles.Success.Bold(true)
	case state.Configured && state.LastTest != nil && !state.LastTest.OK:
		return styles.Danger.Bold(true)
	case state.Configured:
		return styles.Success
	default:
		return styles.Muted
	}
}

func (s *searchConfigState) providerListStatus(id websearch.ProviderID) string {
	state := websearch.Resolve(id, s.settings, s.auth)
	switch {
	case state.Available:
		return "ACTIVO"
	case state.Configured:
		return "CONFIGURADO"
	default:
		return "SIN CONFIGURAR"
	}
}

func (s *searchConfigState) providerListMeta(id websearch.ProviderID) string {
	state := websearch.Resolve(id, s.settings, s.auth)
	parts := []string{}
	switch {
	case !state.Configured:
		return "API key no configurada"
	case !state.EnabledByUser:
		parts = append(parts, "Configurado", "deshabilitado")
	case state.LastTest != nil && !state.LastTest.OK:
		parts = append(parts, "Configurado", "error de validación")
	case !state.Validated:
		parts = append(parts, "Configurado", "pendiente de validar")
	default:
		parts = append(parts, "Configurado", "validado y habilitado")
	}
	if id == s.settings.DefaultProvider && state.Available {
		parts = append(parts, "predeterminado")
	}
	return strings.Join(parts, " · ")
}

func (s *searchConfigState) appendProviderLayout(canvas *settingsCanvas, width int) {
	styles := s.ctx.Styles
	provider := s.selected
	state := websearch.Resolve(provider, s.settings, s.auth)
	available := websearch.AvailableOrder(s.settings, s.auth)

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
		settingsButtonSpec{ID: "search-detail-back", Label: "Volver a motores", Focused: s.focus == "search-detail-back"},
	))
	if s.busy {
		canvas.blank()
		canvas.line(styles.Accent.Render("Probando conexión..."))
	}
	s.appendMessages(canvas, width)
	canvas.blank()
	canvas.block(settingsFooter(styles, "↑↓ mover foco · Enter usar · Esc volver a motores"))
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
	canvas.block(settingsFooter(styles, "Enter guardar y probar · Esc volver a "+websearch.Labels[provider]))
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
	return s.configuredCount() > 0
}

func (s *searchConfigState) configuredCount() int {
	count := 0
	for _, provider := range websearch.ProviderIDs {
		if strings.TrimSpace(s.auth.APIKeys[provider]) != "" {
			count++
		}
	}
	return count
}
