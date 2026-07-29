package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderScrollbar dibuja una barra vertical con la posición actual del
// viewport. Inspirada en las scrollbars de opencode / pi.dev: canal punteado
// + perilla llena que se puede visualizar en cualquier terminal. No es
// interactiva con el mouse (el propio bubble tea ya soporta rueda), pero
// da retroalimentación visual mientras el usuario navega con PgUp/PgDown.
func (m *ChatModel) renderScrollbar() string {
	return m.renderScrollbarFor(m.viewport)
}

// renderScrollbarFor permite que View dibuje la barra con la altura efectiva
// del frame, aunque la geometría persistida del viewport aún corresponda al
// frame anterior.
func (m *ChatModel) renderScrollbarFor(vp viewport.Model) string {
	h := vp.Height
	if h < 2 {
		return ""
	}
	theme := m.ctx.Styles.Theme
	trackStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	thumbStyle := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)

	total := vp.TotalLineCount()
	visible := h
	// Si todo el contenido cabe en la ventana, no dibujamos perilla: sólo el
	// canal apagado. Así el usuario ve que hay un carril pero no ruido.
	if total <= visible {
		return trackStyle.Render(strings.Repeat("│\n", h-1) + "│")
	}

	// Alto y posición de la perilla proporcionales al contenido.
	thumbH := (visible * h) / total
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > h {
		thumbH = h
	}
	off := vp.YOffset
	maxOff := total - visible
	if maxOff < 1 {
		maxOff = 1
	}
	thumbTop := (off * (h - thumbH)) / maxOff
	if thumbTop < 0 {
		thumbTop = 0
	}
	if thumbTop+thumbH > h {
		thumbTop = h - thumbH
	}

	var b strings.Builder
	for i := 0; i < h; i++ {
		if i >= thumbTop && i < thumbTop+thumbH {
			b.WriteString(thumbStyle.Render("█"))
		} else {
			b.WriteString(trackStyle.Render("│"))
		}
		if i < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
