package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
)

func newInputTestChat(t *testing.T) *ChatModel {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "data: [DONE]")
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	ctx := &AppContext{
		ConfigDir: dir,
		Styles:    NewStyles(DefaultTheme()),
		Providers: providers.Config{
			ActiveProviderID: "test",
			ActiveModelID:    "modelo",
			Providers: []providers.Provider{{
				ID:      "test",
				Name:    "Test",
				BaseURL: server.URL,
				Auth:    providers.AuthNone,
				Models:  []providers.Model{{ID: "modelo", MaxContextTokens: 100_000}},
			}},
		},
		Client: &openai.Client{HTTP: server.Client(), Dir: dir},
	}
	m := NewChat(ctx)
	m.Resize(100, 30)
	t.Cleanup(func() {
		if m.cancel != nil {
			m.cancel()
		}
	})
	return &m
}

func TestRunTurnNoMuestraLilithAntesDelPrimerDelta(t *testing.T) {
	m := newInputTestChat(t)

	_, cmd := m.submit("hola")
	if cmd == nil {
		t.Fatal("el envío debe iniciar el turno")
	}
	if !m.streaming || !m.thinking {
		t.Fatalf("el turno debe quedar esperando respuesta: streaming=%v thinking=%v", m.streaming, m.thinking)
	}
	for _, msg := range m.messages {
		if msg.Kind == MsgAssistant {
			t.Fatal("no debe existir una respuesta assistant vacía antes del primer delta")
		}
	}

	view := stripANSI(m.View())
	if strings.Contains(view, "» lilith") {
		t.Fatalf("la cabecera de lilith apareció antes de recibir contenido:\n%s", view)
	}
	if !strings.Contains(view, "Pensando") {
		t.Fatalf("la espera debe seguir mostrando Pensando:\n%s", view)
	}
}

func TestEnterInmediatoDespuesDeEscribirEnvia(t *testing.T) {
	m := newInputTestChat(t)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter inmediato debe enviar, no insertar un salto de línea")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("el textarea debe limpiarse tras enviar, obtuvo %q", got)
	}
	if len(m.messages) == 0 || m.messages[0].Kind != MsgUser || m.messages[0].Content != "x" {
		t.Fatalf("mensaje de usuario inesperado: %#v", m.messages)
	}
}

func TestBracketedPasteMultilineaSeInsertaComoUnBloque(t *testing.T) {
	m := newInputTestChat(t)
	pasted := "primera línea\r\nsegunda línea\r\ntercera línea"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	if cmd != nil {
		t.Fatal("un paste bracketed no necesita tareas asíncronas para simular escritura")
	}
	want := "primera línea\nsegunda línea\ntercera línea"
	if got := m.textarea.Value(); got != want {
		t.Fatalf("paste inesperado:\nwant=%q\n got=%q", want, got)
	}
	if len(m.messages) != 0 {
		t.Fatalf("los saltos del paste no deben disparar solicitudes: %#v", m.messages)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.messages) != 1 || m.messages[0].Kind != MsgUser || m.messages[0].Content != want {
		t.Fatalf("el bloque pegado debe enviarse como un único mensaje: %#v", m.messages)
	}
}

func TestReasoningSeMuestraAntesDeLaRespuestaTextual(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: "prueba", Time: time.Now()})
	primeTestRequest(t, &m)
	m.thinking = true
	m.assistantActive = -1
	m.lastTranscriptRefresh = time.Time{}

	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{thinking: "Analizando el proyecto..."}))
	view := stripANSI(m.View())
	if !strings.Contains(view, "Analizando el proyecto") {
		t.Fatalf("el razonamiento debe mostrarse en vivo:\n%s", view)
	}
	if strings.Contains(view, "» lilith") {
		t.Fatalf("el razonamiento no debe crear una respuesta textual vacía:\n%s", view)
	}
	if got := m.reasoningBuf.String(); got != "Analizando el proyecto..." {
		t.Fatalf("buffer de reasoning inesperado: %q", got)
	}

	var panel *ThinkingPanel
	for _, msg := range m.messages {
		if msg.Kind == MsgThinking {
			panel = msg.Thinking
			break
		}
	}
	if panel == nil {
		t.Fatal("faltó el panel de razonamiento")
	}

	m.lastTranscriptRefresh = time.Time{}
	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{delta: "Respuesta"}))
	if !panel.Done {
		t.Fatal("el panel de razonamiento debe finalizar al comenzar la respuesta")
	}
	if m.assistantActive < 0 || m.messages[m.assistantActive].Content != "Respuesta" {
		t.Fatalf("el primer delta debe materializar la respuesta de lilith: %#v", m.messages)
	}
	if !strings.Contains(stripANSI(m.View()), "» lilith") {
		t.Fatal("la cabecera de lilith debe aparecer desde el primer delta textual")
	}
}

func TestReasoningDeToolCallSeConservaEnHistorial(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	primeTestRequest(t, &m)
	m.thinking = true
	m.assistantActive = -1

	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{thinking: "Necesito inspeccionar el archivo."}))
	call := makeToolCall("read_files", `{"paths":["README.md"]}`)
	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{toolCalls: []openai.ToolCall{call}}))
	_, cmd := m.Update(activeStreamMsg(&m, chatStreamMsg{done: true}))
	if cmd == nil {
		t.Fatal("una tool call final debe iniciar su ejecución")
	}
	if len(m.history) != 1 {
		t.Fatalf("historial inesperado: %#v", m.history)
	}
	if got := m.history[0].ReasoningContent; got != "Necesito inspeccionar el archivo." {
		t.Fatalf("reasoning no preservado junto a la tool call: %q", got)
	}
}
