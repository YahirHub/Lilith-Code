package textinput

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/tui/uikit"
	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

func TestSetValueLeavesCursorAtEnd(t *testing.T) {
	model := New()
	model.SetValue("abc")
	model.Focus()
	model, _ = model.Update(uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune("d")})
	if got := model.Value(); got != "abcd" {
		t.Fatalf("la inserción no continuó al final: %q", got)
	}
}

func TestFitPreservesANSIWhileTruncating(t *testing.T) {
	styled := tuistyle.NewStyle().Foreground(tuistyle.Color("#ff0000")).Render("abcdef")
	got := fit(styled, 4)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("fit perdió el estilo ANSI: %q", got)
	}
	if width := termansi.StringWidth(got); width > 4 {
		t.Fatalf("fit produjo %d celdas: %q", width, got)
	}
	if plain := termansi.Strip(got); plain != "abcd" {
		t.Fatalf("contenido truncado inesperado: %q", plain)
	}
}
