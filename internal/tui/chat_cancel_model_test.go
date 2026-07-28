package tui

import (
	"testing"
	"time"

	"github.com/lilith/li/internal/providers/openai"
)

func TestCancelTurnIsImmediateAndInvalidatesLateToolResult(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	oldTurn := m.activeTurnID
	done := m.turnCtx.Done()

	call := makeToolCall("run_terminal_command", `{"command":"sleep 30"}`)
	m.appendHistory(openai.Message{Role: "assistant", ToolCalls: []openai.ToolCall{call}})
	m.runningCalls = []openai.ToolCall{call}
	cp := &CommandPanel{CallID: call.ID, StartedAt: time.Now()}
	m.cmdPanels = map[int]*CommandPanel{call.Index: cp}
	m.cmdByCall = map[string]*CommandPanel{call.ID: cp}

	start := time.Now()
	m.cancelTurn()
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("cancelTurn bloqueó el Update demasiado: %s", elapsed)
	}
	select {
	case <-done:
	default:
		t.Fatal("Ctrl+C debe cancelar el contexto del turno de inmediato")
	}
	if m.streaming || m.thinking || m.working || m.activeTurnID != 0 {
		t.Fatalf("turno no quedó detenido: streaming=%v thinking=%v working=%v id=%d", m.streaming, m.thinking, m.working, m.activeTurnID)
	}
	if !cp.Done || !cp.Canceled {
		t.Fatalf("el panel debe reflejar cancelación inmediatamente: %+v", cp)
	}
	before := len(m.history)
	_, cmd := m.Update(toolResultsMsg{
		turnID:  oldTurn,
		results: []openai.Message{{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: "exit_code: 1"}},
	})
	if cmd != nil {
		t.Fatal("un resultado tardío de un turno cancelado no debe iniciar otro ciclo")
	}
	if len(m.history) != before {
		t.Fatalf("resultado tardío contaminó el historial: before=%d after=%d", before, len(m.history))
	}
	messageCount := len(m.messages)
	_, cmd = m.Update(chatStreamMsg{turnID: oldTurn, delta: "respuesta tardía"})
	if cmd != nil || len(m.messages) != messageCount {
		t.Fatal("un delta SSE tardío de un turno cancelado debe ignorarse")
	}
}

func TestBeginTurnUsesLatestModelOnlyOnNextUserTurn(t *testing.T) {
	m := newInputTestChat(t)
	m.ctx.Providers.Providers[0].Models = append(m.ctx.Providers.Providers[0].Models,
		m.ctx.Providers.Providers[0].Models[0],
	)
	m.ctx.Providers.Providers[0].Models[1].ID = "deepseek-v4-flash"
	m.ctx.Providers.ActiveModelID = "modelo"

	if err := m.beginTurn(); err != nil {
		t.Fatalf("primer beginTurn: %v", err)
	}
	if got := m.turnModel; got != "modelo" {
		t.Fatalf("modelo inicial = %q", got)
	}

	// Simula una selección hecha por /models mientras existe un turno. El
	// turno actual conserva su snapshot; la selección se aplica al siguiente.
	m.ctx.Providers.ActiveModelID = "deepseek-v4-flash"
	if got := m.turnModel; got != "modelo" {
		t.Fatalf("el turno actual cambió de modelo a mitad de ejecución: %q", got)
	}
	m.endTurn()
	m.streaming = false

	if err := m.beginTurn(); err != nil {
		t.Fatalf("segundo beginTurn: %v", err)
	}
	if got := m.turnModel; got != "deepseek-v4-flash" {
		t.Fatalf("la siguiente petición no tomó el modelo nuevo: %q", got)
	}
	m.endTurn()
}
