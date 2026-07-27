package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

func TestVisualInputLineCountCuentaWrapPorAncho(t *testing.T) {
	if got := visualInputLineCount(strings.Repeat("x", 30), 10, 8); got < 3 {
		t.Fatalf("esperaba al menos 3 líneas visuales, obtuvo %d", got)
	}
	if got := visualInputLineCount("uno\ndos", 80, 8); got != 2 {
		t.Fatalf("esperaba 2 líneas lógicas, obtuvo %d", got)
	}
	if got := visualInputLineCount(strings.Repeat("x", 500), 10, 8); got != 8 {
		t.Fatalf("debe respetar MaxHeight=8, obtuvo %d", got)
	}
}

func TestResizeReservaElAltoRenderizadoDeLaEntrada(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(80, 24)

	if got := m.viewport.Height + m.bottomChromeHeight(80); got != 24 {
		t.Fatalf("layout inicial no cuadra con terminal: %d", got)
	}

	m.textarea.SetValue(strings.Repeat("mensaje largo ", 30))
	m.syncInputHeight()

	if m.textarea.Height() <= 1 {
		t.Fatalf("el textarea no creció con wrap visual: %d", m.textarea.Height())
	}
	if got := m.viewport.Height + m.bottomChromeHeight(80); got != 24 {
		t.Fatalf("layout con entrada envuelta no cuadra con terminal: %d", got)
	}
}

func TestTextareaAutoresizeNoOcultaPrimeraLineaEnvuelta(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(80, 24)

	input := strings.Repeat("mensaje largo ", 10)
	for _, r := range input {
		if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}); cmd != nil {
			_ = cmd
		}
	}

	view := stripANSI(m.inputBoxView(80))
	if !strings.Contains(view, "mensaje largo mensaje largo mensaje largo") {
		t.Fatalf("la primera línea envuelta desapareció del textarea:\n%s", view)
	}
	if !strings.Contains(view, strings.TrimSpace(input[len(input)-28:])) {
		t.Fatalf("la última línea envuelta desapareció del textarea:\n%s", view)
	}
}

func TestRefreshTranscriptEnvuelveAntesDeEnviarAlViewport(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(50, 18)
	m.messages = []ChatMessage{{Kind: MsgAssistant, Content: strings.Repeat("respuesta-larga ", 20)}}
	m.refreshTranscript(true)

	view := stripANSI(m.viewport.View())
	for _, line := range strings.Split(view, "\n") {
		if width := len([]rune(line)); width > m.viewport.Width {
			t.Fatalf("viewport recibió una línea sin envolver: ancho=%d límite=%d línea=%q", width, m.viewport.Width, line)
		}
	}
}
