package tui

import (
	"context"
	"strings"
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
	m.requestSeq++
	m.activeRequestID = m.requestSeq
	oldRequest := m.activeRequestID
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
		t.Fatal("Escape debe cancelar el contexto del turno de inmediato")
	}
	if m.streaming || m.thinking || m.working || m.activeTurnID != 0 || m.activeRequestID != 0 {
		t.Fatalf("turno no quedó detenido: streaming=%v thinking=%v working=%v turn=%d request=%d", m.streaming, m.thinking, m.working, m.activeTurnID, m.activeRequestID)
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
	// The previous guard accepted turnID==0. Keep anonymous async results
	// invalid too; otherwise an edge path can revive a canceled agent.
	_, cmd = m.Update(toolResultsMsg{
		turnID:  0,
		results: []openai.Message{{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: "exit_code: 0"}},
	})
	if cmd != nil || len(m.history) != before || m.thinking || m.working {
		t.Fatal("un resultado asíncrono sin turnID válido debe ignorarse después de cancelar")
	}
	messageCount := len(m.messages)
	_, cmd = m.Update(chatStreamMsg{turnID: oldTurn, requestID: oldRequest, delta: "respuesta tardía"})
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

func TestStaleRequestFromSameTurnCannotKillReplacementRequest(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	turnID := m.activeTurnID
	m.requestSeq++
	oldRequest := m.requestSeq
	m.activeRequestID = oldRequest

	// Simula el preflight de una create_file: corta sólo el request HTTP viejo y
	// abre una continuación dentro del MISMO turno.
	m.requestSeq++
	newRequest := m.requestSeq
	m.activeRequestID = newRequest
	m.thinking = true
	m.streaming = true

	beforeMessages := len(m.messages)
	_, cmd := m.Update(chatStreamMsg{
		turnID:    turnID,
		requestID: oldRequest,
		err:       context.Canceled,
	})
	if cmd != nil {
		t.Fatal("el cierre tardío del request viejo no debe programar trabajo")
	}
	if m.activeTurnID != turnID || m.activeRequestID != newRequest || !m.streaming {
		t.Fatalf("el request viejo mató la continuación: turn=%d request=%d streaming=%v", m.activeTurnID, m.activeRequestID, m.streaming)
	}
	if len(m.messages) != beforeMessages {
		t.Fatal("el request viejo no debe añadir errores ni mensajes")
	}

	_, _ = m.Update(chatStreamMsg{
		turnID:    turnID,
		requestID: newRequest,
		delta:     "continuación válida",
	})
	if m.assistantActive < 0 || !strings.Contains(m.messages[m.assistantActive].Content, "continuación válida") {
		t.Fatalf("el request de reemplazo debe seguir aceptando deltas: %#v", m.messages)
	}
	m.endTurn()
}

func TestCancelTurnPersistsPartialProgressAndResumeRestoresIt(t *testing.T) {
	m := newInputTestChat(t)
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: "implementa la tarea", Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: "implementa la tarea"})
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	// The user request is the stable base. Everything after this point should be
	// recoverable from the lightweight live checkpoint.
	m.persist()
	m.requestSeq++
	m.activeRequestID = m.requestSeq

	_, _ = m.Update(activeStreamMsg(m, chatStreamMsg{thinking: "Estoy revisando la estructura..."}))
	_, _ = m.Update(activeStreamMsg(m, chatStreamMsg{delta: "Ya encontré el archivo que hay que modificar."}))

	m.cancelTurn()
	loaded, err := m.store.Load(m.project, m.sess.ID)
	if err != nil {
		t.Fatalf("load después de cancelar: %v", err)
	}
	if loaded.Live == nil {
		t.Fatal("La cancelación debe dejar un checkpoint live más nuevo que el snapshot estable")
	}
	var sawThinking, sawAssistant, sawCancel bool
	for _, entry := range loaded.Live.Entries {
		if entry.Thinking != nil && strings.Contains(entry.Thinking.Content, "revisando") {
			sawThinking = true
		}
		if entry.Kind == "assistant" && strings.Contains(entry.Content, "encontré el archivo") {
			sawAssistant = true
		}
		if entry.Kind == "system" && strings.Contains(entry.Content, "Tarea cancelada") {
			sawCancel = true
		}
	}
	if !sawThinking || !sawAssistant || !sawCancel {
		t.Fatalf("checkpoint incompleto: thinking=%v assistant=%v cancel=%v entries=%+v", sawThinking, sawAssistant, sawCancel, loaded.Live.Entries)
	}

	m2 := NewChat(m.ctx)
	m2.Resize(100, 30)
	m2.LoadSession(loaded)
	var restoredThinking, restoredAssistant bool
	for _, msg := range m2.messages {
		if msg.Thinking != nil && strings.Contains(msg.Thinking.Content, "revisando") {
			restoredThinking = true
		}
		if msg.Kind == MsgAssistant && strings.Contains(msg.Content, "encontré el archivo") {
			restoredAssistant = true
		}
	}
	if !restoredThinking || !restoredAssistant {
		t.Fatalf("resume perdió progreso: thinking=%v assistant=%v", restoredThinking, restoredAssistant)
	}
	if len(m2.history) < 2 || m2.history[0].Role != "user" || m2.history[1].Role != "assistant" {
		t.Fatalf("el progreso seguro también debe recuperarse para la siguiente petición: %#v", m2.history)
	}
}
