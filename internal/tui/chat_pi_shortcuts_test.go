package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lilith/li/internal/providers/openai"
)

func TestCtrlCAndCtrlZAreIgnoredAndNeverExit(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	done := m.turnCtx.Done()
	m.enqueue("sigue con esta corrección", queueSteer)
	m.textarea.SetValue("borrador")

	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlZ} {
		_, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd != nil {
			t.Fatalf("%v no debe ejecutar salida ni suspensión", key)
		}
		select {
		case <-done:
			t.Fatalf("%v no debe cancelar el turno activo", key)
		default:
		}
	}
	if !m.streaming || m.activeTurnID == 0 {
		t.Fatal("Ctrl+C/Ctrl+Z deben dejar viva la tarea actual")
	}
	if len(m.queue) != 1 || m.queue[0].Text != "sigue con esta corrección" {
		t.Fatalf("Ctrl+C/Ctrl+Z no deben tocar la cola: %#v", m.queue)
	}
	if got := m.textarea.Value(); got != "borrador" {
		t.Fatalf("Ctrl+C/Ctrl+Z no deben modificar el editor, obtuvo %q", got)
	}
}

func TestExitCommandQuitsImmediatelyWhileStreaming(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}

	_, cmd := m.submit("/exit")
	if cmd == nil {
		t.Fatal("/exit debe devolver el comando de salida")
	}
	if m.streaming || m.activeTurnID != 0 {
		t.Fatalf("/exit debe cancelar el turno antes de salir: streaming=%v turn=%d", m.streaming, m.activeTurnID)
	}
	if len(m.queue) != 0 {
		t.Fatalf("/exit no debe entrar a la cola: %#v", m.queue)
	}
}

func TestEscapeCancelsAndRestoresQueuedMessages(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	done := m.turnCtx.Done()
	m.enqueue("primera corrección", queueSteer)
	m.enqueue("después revisa pruebas", queueFollowUp)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("Escape debe cancelar sin programar un nuevo turno")
	}
	select {
	case <-done:
	default:
		t.Fatal("Escape debe cancelar el contexto raíz de la tarea")
	}
	if m.streaming || m.activeTurnID != 0 {
		t.Fatalf("Escape no detuvo el turno: streaming=%v turn=%d", m.streaming, m.activeTurnID)
	}
	if len(m.queue) != 0 {
		t.Fatalf("Escape debe sacar la cola al editor, quedó %#v", m.queue)
	}
	got := m.textarea.Value()
	if !strings.Contains(got, "primera corrección") || !strings.Contains(got, "después revisa pruebas") {
		t.Fatalf("Escape perdió mensajes encolados: %q", got)
	}
}

func TestAltUpRestoresQueueWithoutCancelingTurn(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	done := m.turnCtx.Done()
	m.enqueue("editar antes de enviar", queueSteer)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	select {
	case <-done:
		t.Fatal("Alt+Up sólo debe recuperar la cola, no cancelar la tarea")
	default:
	}
	if len(m.queue) != 0 || !strings.Contains(m.textarea.Value(), "editar antes de enviar") {
		t.Fatalf("Alt+Up no recuperó la cola correctamente: queue=%#v input=%q", m.queue, m.textarea.Value())
	}
}

func TestAltEnterQueuesFollowUpWhileStreaming(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	m.textarea.SetValue("haz esto cuando termines")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(m.queue) != 1 {
		t.Fatalf("Alt+Enter debe crear un follow-up: %#v", m.queue)
	}
	if m.queue[0].Mode != queueFollowUp || m.queue[0].Text != "haz esto cuando termines" {
		t.Fatalf("follow-up inesperado: %#v", m.queue[0])
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("Alt+Enter debe limpiar el editor tras encolar, obtuvo %q", got)
	}
}

func TestSteeringIsDeliveredAtToolBoundary(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	turnID := m.activeTurnID
	m.enqueue("cambia el enfoque antes de seguir", queueSteer)

	_, cmd := m.Update(toolResultsMsg{
		turnID:  turnID,
		results: []openai.Message{{Role: "tool", ToolCallID: "call-1", Name: "read_files", Content: "ok"}},
	})
	if cmd == nil {
		t.Fatal("tras una frontera de tools debe continuar el mismo turno")
	}
	if m.activeTurnID != turnID || !m.streaming {
		t.Fatalf("steering abrió/cerró el turno incorrectamente: turn=%d streaming=%v", m.activeTurnID, m.streaming)
	}
	if len(m.queue) != 0 {
		t.Fatalf("el steering debe consumirse en la frontera de tools: %#v", m.queue)
	}
	if len(m.history) == 0 || m.history[len(m.history)-1].Role != "user" || m.history[len(m.history)-1].Content != "cambia el enfoque antes de seguir" {
		t.Fatalf("el steering no llegó al historial del request siguiente: %#v", m.history)
	}
}
