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

func TestViewKeepsCursorVisibleAtEndWithoutTruncatingValue(t *testing.T) {
	model := New()
	model.Width = 12
	endpoint := "https://example.com/v1/chat/completions"
	model.SetValue(endpoint)
	model.Focus()

	plain := termansi.Strip(model.View())
	if !strings.HasSuffix(plain, "completions▌") {
		t.Fatalf("el viewport no siguió el cursor al final: %q", plain)
	}
	if got := model.Value(); got != endpoint {
		t.Fatalf("View alteró el valor almacenado: %q", got)
	}
	if width := termansi.StringWidth(plain); width > termansi.StringWidth(model.Prompt)+model.Width {
		t.Fatalf("View excedió el ancho: %d celdas en %q", width, plain)
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
