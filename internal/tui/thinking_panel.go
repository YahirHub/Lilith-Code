package tui

import (
	"strconv"
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// ThinkingPanel muestra el resumen de razonamiento del modelo (reasoning
// summary) con una vista compacta que crece con el contenido hasta su límite
// histórico y conserva el toggle expandir/plegar como los file panels.
type ThinkingPanel struct {
	Content  string
	Done     bool
	Expanded bool
}

// thinkingPreviewLines limita la altura máxima del contenido visible.
const thinkingPreviewLines = 6

func (p *ThinkingPanel) Append(s string) { p.Content += s }
func (p *ThinkingPanel) Finish()         { p.Done = true }

func (p *ThinkingPanel) View(s Styles, width int, selected bool) string {
	if width < 24 {
		width = 24
	}
	inner := width - 4
	t := s.Theme

	arrow := "▾"
	if !p.Expanded {
		arrow = "▸"
	}
	title := "Pensando"
	if p.Done {
		title = "Pensó"
	}
	head := tuistyle.NewStyle().Foreground(t.Primary).Bold(true).Render(arrow + " " + title)
	if !p.Done {
		head += "  " + s.Muted.Render("razonando…")
	}
	hint := "(ctrl+r ocultar)"
	if !p.Expanded {
		hint = "(ctrl+r expandir)"
	}
	head += "  " + s.Muted.Render(hint)

	lines := wrapThinking(p.Content, inner)
	body := head
	if p.Expanded && len(lines) > 0 {
		view, hidden := cappedTailPreview(lines, thinkingPreviewLines)
		if hidden > 0 {
			view = append([]string{s.Muted.Render("… " + strconv.Itoa(hidden) + " líneas más arriba")}, view...)
		}
		muted := tuistyle.NewStyle().Foreground(t.Muted).Italic(true)
		body += "\n" + muted.Render(strings.Join(view, "\n"))
	}

	border := t.Border
	if selected {
		border = t.Primary
	}
	return tuistyle.NewStyle().
		Border(tuistyle.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width - 2).
		Render(body)
}

// wrapThinking envuelve por ancho monoespacio (aproximación por runas).
func wrapThinking(s string, width int) []string {
	if s == "" {
		return nil
	}
	if width < 8 {
		width = 8
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}
		runes := []rune(line)
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return out
}
