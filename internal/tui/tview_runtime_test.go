package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTViewSurfacePasteIsAtomic(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	surface := newTViewModelSurface(func(msg tea.Msg) { messages <- msg })

	surface.PasteHandler()("primera\r\nsegunda\rtercera", nil)

	select {
	case raw := <-messages:
		msg, ok := raw.(tea.KeyMsg)
		if !ok {
			t.Fatalf("se esperaba tea.KeyMsg, se obtuvo %T", raw)
		}
		if !msg.Paste {
			t.Fatal("el bloque debe conservar Paste=true")
		}
		if got, want := string(msg.Runes), "primera\nsegunda\ntercera"; got != want {
			t.Fatalf("pegado normalizado inesperado: got %q want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("el PasteHandler no publicó el bloque")
	}
}

func TestTViewSurfacePreservesSpaceRune(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	surface := newTViewModelSurface(func(msg tea.Msg) { messages <- msg })

	surface.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), nil)

	select {
	case raw := <-messages:
		msg, ok := raw.(tea.KeyMsg)
		if !ok {
			t.Fatalf("se esperaba tea.KeyMsg, se obtuvo %T", raw)
		}
		if msg.Type != tea.KeySpace || string(msg.Runes) != " " {
			t.Fatalf("espacio inválido: type=%v runes=%q", msg.Type, string(msg.Runes))
		}
	case <-time.After(time.Second):
		t.Fatal("el InputHandler no publicó el espacio")
	}
}

func TestTViewSurfaceAcceptsLargeMultilinePaste(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	surface := newTViewModelSurface(func(msg tea.Msg) { messages <- msg })
	lines := make([]string, 64)
	for index := range lines {
		lines[index] = strings.Repeat(string(rune('a'+index%26)), 96)
	}
	pasted := strings.Join(lines, "\r\n")

	surface.PasteHandler()(pasted, nil)

	select {
	case raw := <-messages:
		msg := raw.(tea.KeyMsg)
		got := string(msg.Runes)
		if strings.Count(got, "\n") != len(lines)-1 {
			t.Fatalf("líneas perdidas: got=%d want=%d", strings.Count(got, "\n")+1, len(lines))
		}
		if len([]rune(got)) <= 5000 {
			t.Fatalf("la prueba debe superar 5000 runes, obtuvo %d", len([]rune(got)))
		}
		if !strings.HasSuffix(got, lines[len(lines)-1]) {
			t.Fatal("el final del bloque pegado fue truncado")
		}
	case <-time.After(time.Second):
		t.Fatal("el PasteHandler no publicó el bloque largo")
	}
}

func TestTViewMouseDoesNotDuplicateSyntheticClicks(t *testing.T) {
	event := tcell.NewEventMouse(4, 7, tcell.Button1, tcell.ModNone)
	if _, ok := tviewMouseMsg(tview.MouseLeftClick, event); ok {
		t.Fatal("el click sintético debe ignorarse para no duplicar press/release")
	}

	msg, ok := tviewMouseMsg(tview.MouseLeftDown, event)
	if !ok {
		t.Fatal("MouseLeftDown debe traducirse")
	}
	mouse := tea.MouseEvent(msg)
	if mouse.X != 4 || mouse.Y != 7 || mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
		t.Fatalf("traducción de ratón inesperada: %+v", mouse)
	}
}

func TestTViewRuntimeDispatchesBatchCommands(t *testing.T) {
	runtime := &tviewRuntime{
		messages: newTViewMessageQueue(),
		stopped:  make(chan struct{}),
	}
	runtime.dispatch(tea.Batch(
		func() tea.Msg { return systemMsg{text: "uno"} },
		func() tea.Msg { return systemMsg{text: "dos"} },
	))

	seen := map[string]bool{}
	for len(seen) < 2 {
		timedOut := make(chan struct{})
		go func() {
			time.Sleep(time.Second)
			close(timedOut)
		}()
		raw, ok := runtime.messages.pop(timedOut)
		if !ok {
			t.Fatalf("batch incompleto: %+v", seen)
		}
		msg, ok := raw.(systemMsg)
		if !ok {
			t.Fatalf("mensaje inesperado: %T", raw)
		}
		seen[msg.text] = true
	}
}

func TestForwardTViewCtrlCClonesEvent(t *testing.T) {
	original := tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl)
	forwarded := forwardTViewCtrlC(original)
	if forwarded == original {
		t.Fatal("Ctrl+C debe clonarse para evitar el cierre automático de tview")
	}
	if forwarded.Key() != tcell.KeyCtrlC || forwarded.Modifiers() != tcell.ModCtrl {
		t.Fatalf("Ctrl+C clonado inesperado: key=%v modifiers=%v", forwarded.Key(), forwarded.Modifiers())
	}

	regular := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if got := forwardTViewCtrlC(regular); got != regular {
		t.Fatal("las teclas normales deben conservar el evento original")
	}
}

func TestTViewMessageQueueIsNonBlockingAndOrdered(t *testing.T) {
	queue := newTViewMessageQueue()
	const total = 10000

	done := make(chan struct{})
	go func() {
		for index := 0; index < total; index++ {
			queue.push(systemMsg{text: fmt.Sprintf("%05d", index)})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("la cola bloqueó al productor")
	}

	stopped := make(chan struct{})
	defer close(stopped)
	for index := 0; index < total; index++ {
		raw, ok := queue.pop(stopped)
		if !ok {
			t.Fatalf("la cola terminó en %d", index)
		}
		msg, ok := raw.(systemMsg)
		if !ok {
			t.Fatalf("mensaje inesperado: %T", raw)
		}
		if want := fmt.Sprintf("%05d", index); msg.text != want {
			t.Fatalf("orden alterado en %d: got=%q want=%q", index, msg.text, want)
		}
	}
}
