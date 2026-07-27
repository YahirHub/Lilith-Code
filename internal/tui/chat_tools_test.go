package tui

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers/openai"
)

func makeToolCall(name, args string) openai.ToolCall {
	var call openai.ToolCall
	call.ID = "call-test"
	call.Index = 0
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

func TestSummarizeToolResultsUsesWriteOutput(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "write_file",
		Content: "Escrito ejemplo.html (1121 bytes).",
	}})
	if got != "Escrito ejemplo.html (1121 bytes)." {
		t.Fatalf("resumen inesperado: %q", got)
	}
}

func TestSummarizeToolResultsKeepsToolErrors(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "read_files",
		Content: "error: archivo no encontrado",
	}})
	if !strings.Contains(got, "archivo no encontrado") {
		t.Fatalf("debe conservar errores de herramientas: %q", got)
	}
}

func TestSummarizeToolResultsIgnoresReadOnlyNoise(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "read_files",
		Content: "== README.md ==\ncontenido largo",
	}})
	if got != "" {
		t.Fatalf("no debe convertir lecturas normales en respuesta final: %q", got)
	}
}

func TestPartialToolCallShowsWorkingImmediately(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	m.streaming = true
	m.thinking = true

	_, _ = m.Update(chatStreamMsg{
		toolCalls: []openai.ToolCall{makeToolCall("write_file", `{"path":"demo.html","content":"<h1>hola</h1>"}`)},
		partial:   true,
	})

	if m.thinking {
		t.Fatal("una tool call parcial debe salir del estado pensando")
	}
	if !m.working {
		t.Fatal("una tool call parcial debe activar trabajando desde el primer snapshot")
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "Trabajando") {
		t.Fatalf("el indicador fijo debe seguir visible durante argumentos parciales:\n%s", view)
	}

	// El timer del shimmer no debe morir durante el intervalo parcial. Éste
	// era el fallo real: thinking=false + working=false hacía que el siguiente
	// tick devolviera nil y la animación se apagara hasta finalizar la call.
	_, cmd := m.Update(thinkingTickMsg{frame: 1})
	if cmd == nil {
		t.Fatal("el shimmer debe seguir programando frames mientras working=true")
	}
}

func TestReasoningKeepsThinkingIndicatorActive(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	m.streaming = true
	m.thinking = true

	_, _ = m.Update(chatStreamMsg{thinking: "Analizando el cambio..."})

	if !m.thinking || m.working {
		t.Fatalf("reasoning debe mantener Pensando activo: thinking=%v working=%v", m.thinking, m.working)
	}
	if !strings.Contains(stripANSI(m.View()), "Pensando") {
		t.Fatal("el indicador Pensando debe permanecer visible mientras llega reasoning")
	}
}
