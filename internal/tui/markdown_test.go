package tui

import (
	"strings"
	"testing"

	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
)

func TestRenderMarkdownPreservesInlineStylesWhenWrapping(t *testing.T) {
	got := RenderMarkdown("Texto **importante** con `codigo` que debe continuar en otra fila.", 20)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("el Markdown perdió los estilos ANSI: %q", got)
	}
	plain := termansi.Strip(got)
	for _, want := range []string{"Texto", "importante", "codigo", "otra fila"} {
		if !strings.Contains(strings.ReplaceAll(plain, "\n", " "), want) {
			t.Fatalf("falta %q en el Markdown renderizado: %q", want, plain)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if width := termansi.StringWidth(line); width > 20 {
			t.Fatalf("fila Markdown de %d celdas excede el límite: %q", width, line)
		}
	}
}
