package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	litodo "github.com/lilith/li/internal/todo"
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

func TestChatViewOcupaTodoElAltoConChromeDinamico(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.working = true
	m.Resize(100, 36)

	assertHeight := func(label string) {
		t.Helper()
		if got := lipgloss.Height(m.View()); got != 36 {
			t.Fatalf("%s: View debe ocupar las 36 filas de la terminal, obtuvo %d", label, got)
		}
	}
	assertHeight("actividad visible")

	// La actividad puede desaparecer al completar/cancelar un turno sin que el
	// terminal emita WindowSizeMsg. Antes dejaba reservadas sus filas y el
	// status bar quedaba flotando sobre un bloque negro al fondo.
	m.working = false
	assertHeight("actividad retirada sin resize")

	// TodoWrite también puede aparecer entre dos frames con la misma geometría.
	err := m.todos.Restore(&litodo.State{
		SchemaVersion: litodo.SchemaVersion,
		Revision:      1,
		Tasks: []litodo.Task{
			{Key: "inspect", Subject: "Revisar proyecto", Status: litodo.Completed},
			{Key: "implement", Subject: "Implementar cambio", Status: litodo.InProgress},
			{Key: "verify", Subject: "Verificar resultado", Status: litodo.Pending},
		},
	})
	if err != nil {
		t.Fatalf("restaurar todo de prueba: %v", err)
	}
	assertHeight("TodoWrite agregado sin resize")

	m.todos.Reset()
	assertHeight("TodoWrite retirado sin resize")
}

func TestScrollManualOcultaTodoInputYStatusYUsaTodaLaTerminal(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(90, 28)
	for i := 0; i < 40; i++ {
		m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: "línea histórica para permitir desplazamiento", Time: time.Now()})
	}
	if err := m.todos.Restore(&litodo.State{
		SchemaVersion: litodo.SchemaVersion,
		Revision:      1,
		Tasks: []litodo.Task{
			{Key: "a", Subject: "Primera tarea", Status: litodo.Completed},
			{Key: "b", Subject: "Segunda tarea", Status: litodo.InProgress},
			{Key: "c", Subject: "Tercera tarea", Status: litodo.Pending},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m.refreshTranscript(true)
	bottom := stripANSI(m.View())
	if !strings.Contains(bottom, "Tareas 1/3") || !strings.Contains(bottom, "Escribe un mensaje") {
		t.Fatalf("at bottom interaction tail must be visible:\n%s", bottom)
	}

	m.viewport.LineUp(8)
	m.userScrolled = true
	reading := stripANSI(m.View())
	if strings.Contains(reading, "Tareas 1/3") || strings.Contains(reading, "Escribe un mensaje") || strings.Contains(reading, "sin proveedor") {
		t.Fatalf("reading older transcript must not keep bottom chrome pinned:\n%s", reading)
	}
	if got := lipgloss.Height(m.View()); got != 28 {
		t.Fatalf("full-height reading mode should consume 28 rows, got %d", got)
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

func TestThinkingTickNoReconstruyeTranscript(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(80, 24)
	m.messages = []ChatMessage{{Kind: MsgAssistant, Content: "respuesta estable", Time: time.Now()}}
	m.refreshTranscript(true)
	m.thinking = true
	before := m.lastTranscriptRefresh

	_, cmd := m.Update(thinkingTickMsg{frame: 2})
	if cmd == nil {
		t.Fatal("el shimmer debe seguir animándose")
	}
	if !m.lastTranscriptRefresh.Equal(before) {
		t.Fatal("un frame del shimmer no debe reconstruir el viewport")
	}
}

func TestRefreshTranscriptConservaScrollManualConHistorialLargo(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(80, 20)
	for i := 0; i < 80; i++ {
		m.messages = append(m.messages, ChatMessage{
			Kind:    MsgAssistant,
			Content: "mensaje histórico con varias palabras para ocupar espacio",
			Time:    time.Now(),
		})
	}
	m.refreshTranscript(true)
	if !m.viewport.AtBottom() {
		t.Fatal("el transcript inicial debe quedar al fondo")
	}
	m.viewport.LineUp(12)
	m.userScrolled = true
	offset := m.viewport.YOffset
	total := m.viewport.TotalLineCount()

	m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: "mensaje nuevo", Time: time.Now()})
	m.refreshTranscript(true)

	if m.viewport.YOffset != offset {
		t.Fatalf("el auto-scroll movió al usuario: antes=%d después=%d", offset, m.viewport.YOffset)
	}
	if m.viewport.TotalLineCount() <= total {
		t.Fatal("el mensaje nuevo debe agregarse sin recortar el historial anterior")
	}
}

func TestRefreshStreamingAgrupaPintadosRapidos(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(80, 20)
	m.streaming = true
	m.messages = []ChatMessage{
		{Kind: MsgUser, Content: "hola", Time: time.Now()},
		{Kind: MsgAssistant, Content: "respuesta", Time: time.Now()},
	}
	m.refreshTranscript(true)
	m.lastTranscriptRefresh = time.Now()
	before := m.lastTranscriptRefresh

	cmd := m.refreshTranscriptStreaming(true)
	if cmd == nil {
		t.Fatal("un refresco demasiado cercano debe programarse, no ejecutarse inmediatamente")
	}
	if !m.transcriptRefreshPending {
		t.Fatal("debe quedar un refresco agrupado pendiente")
	}
	if !m.lastTranscriptRefresh.Equal(before) {
		t.Fatal("la llamada agrupada no debe reconstruir el viewport de inmediato")
	}
}

func TestContextUsageSeCacheaYSeInvalidaConHistorial(t *testing.T) {
	ctx := &AppContext{
		ConfigDir: t.TempDir(),
		Styles:    NewStyles(DefaultTheme()),
		Providers: providers.Config{
			ActiveProviderID: "test",
			ActiveModelID:    "modelo",
			Providers: []providers.Provider{{
				ID: "test",
				Models: []providers.Model{{
					ID:               "modelo",
					MaxContextTokens: 100_000,
				}},
			}},
		},
	}
	m := NewChat(ctx)
	m.appendHistory(openai.Message{Role: "user", Content: strings.Repeat("a", 400)})
	used1, max1 := m.contextUsage()
	if used1 <= 0 || max1 != 100_000 {
		t.Fatalf("uso inicial inesperado: used=%d max=%d", used1, max1)
	}
	if m.contextCacheDirty {
		t.Fatal("el cálculo debe dejar la caché limpia")
	}

	// Sin invalidación explícita, una segunda lectura debe usar el valor ya
	// calculado. Las mutaciones reales pasan por appendHistory/Clear/LoadSession.
	m.history = append(m.history, openai.Message{Role: "user", Content: strings.Repeat("b", 4_000)})
	usedCached, _ := m.contextUsage()
	if usedCached != used1 {
		t.Fatal("contextUsage volvió a recorrer el historial pese a tener caché válida")
	}

	m.invalidateContextUsage()
	used2, _ := m.contextUsage()
	if used2 <= used1 {
		t.Fatalf("tras invalidar debía reflejar el historial nuevo: antes=%d después=%d", used1, used2)
	}
}
