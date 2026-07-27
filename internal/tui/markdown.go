package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// renderer cache keyed by width so glamour compila el estilo una sola vez.
var (
	mdMu       sync.Mutex
	mdRenderer *glamour.TermRenderer
	mdWidth    int
)

// RenderMarkdown convierte texto Markdown en salida ANSI usando glamour.
// Si glamour falla o el contenido está vacío, devuelve el texto original.
func RenderMarkdown(src string, width int) string {
	src = strings.TrimRight(src, " \n\t")
	if src == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	mdMu.Lock()
	if mdRenderer == nil || mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			mdMu.Unlock()
			return src
		}
		mdRenderer = r
		mdWidth = width
	}
	r := mdRenderer
	mdMu.Unlock()

	out, err := r.Render(src)
	if err != nil {
		return src
	}
	// glamour añade padding vertical; recortamos para no romper el layout.
	return strings.Trim(out, "\n")
}
