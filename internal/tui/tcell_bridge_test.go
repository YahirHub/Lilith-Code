package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lilith/li/internal/tui/uikit"
)

func TestTcellPasteIsAtomic(t *testing.T) {
	var input tcellInputState

	if msgs := input.messages(tcell.NewEventPaste(true)); len(msgs) != 0 {
		t.Fatalf("el inicio del pegado no debe emitir mensajes: %#v", msgs)
	}
	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyLF, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone),
	} {
		if msgs := input.messages(event); len(msgs) != 0 {
			t.Fatalf("el contenido intermedio del pegado no debe emitirse: %#v", msgs)
		}
	}

	msgs := input.messages(tcell.NewEventPaste(false))
	if len(msgs) != 1 {
		t.Fatalf("el pegado debe emitirse como un único mensaje, obtuvo %d", len(msgs))
	}
	key, ok := msgs[0].(uikit.KeyMsg)
	if !ok {
		t.Fatalf("el mensaje debe ser uikit.KeyMsg, obtuvo %T", msgs[0])
	}
	if !key.Paste {
		t.Fatal("el mensaje debe conservar Paste=true")
	}
	if got := string(key.Runes); got != "ab\nc" {
		t.Fatalf("contenido pegado inesperado: %q", got)
	}
}

func TestTcellEventBridgeDrainsLargePasteBeforePublishing(t *testing.T) {
	events := make(chan tcell.Event)
	messages := make(chan uikit.Msg)
	quit := make(chan struct{})
	go bridgeTCellEvents(events, messages, quit)

	const pasteSize = 5000
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		events <- tcell.NewEventPaste(true)
		for i := 0; i < pasteSize; i++ {
			events <- tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
		}
		events <- tcell.NewEventPaste(false)
		close(events)
	}()

	// Keep the downstream channel intentionally blocked. A bridge that forwards
	// each pasted rune separately would stop draining the physical event stream
	// and this producer would never finish.
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("el puente dejó de drenar el pegado largo mientras la salida estaba bloqueada")
	}

	select {
	case raw, ok := <-messages:
		if !ok {
			t.Fatal("el puente cerró la salida sin publicar el pegado")
		}
		key, ok := raw.(uikit.KeyMsg)
		if !ok {
			t.Fatalf("el mensaje debe ser uikit.KeyMsg, obtuvo %T", raw)
		}
		if !key.Paste {
			t.Fatal("el pegado largo debe conservar Paste=true")
		}
		if got := string(key.Runes); got != strings.Repeat("x", pasteSize) {
			t.Fatalf("pegado largo truncado: obtuvo %d runes, esperaba %d", len(key.Runes), pasteSize)
		}
	case <-time.After(time.Second):
		t.Fatal("el puente no publicó el pegado largo completo")
	}

	select {
	case _, ok := <-messages:
		if ok {
			t.Fatal("el puente publicó más de un mensaje para un solo pegado")
		}
	case <-time.After(time.Second):
		t.Fatal("el puente no cerró la salida al terminar los eventos")
	}
}

func TestTcellKeyTranslation(t *testing.T) {
	t.Run("control", func(t *testing.T) {
		msg, ok := tcellKeyMsg(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl))
		if !ok {
			t.Fatal("Ctrl+C debe traducirse")
		}
		if msg.Type != uikit.KeyCtrlC {
			t.Fatalf("tipo inesperado: %v", msg.Type)
		}
	})

	t.Run("special keys", func(t *testing.T) {
		tests := []struct {
			name string
			key  tcell.Key
			want uikit.KeyType
		}{
			{name: "enter", key: tcell.KeyEnter, want: uikit.KeyEnter},
			{name: "tab", key: tcell.KeyTab, want: uikit.KeyTab},
			{name: "backspace", key: tcell.KeyBackspace, want: uikit.KeyBackspace},
			{name: "escape", key: tcell.KeyEsc, want: uikit.KeyEsc},
			{name: "ctrl-j", key: tcell.KeyCtrlJ, want: uikit.KeyCtrlJ},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				msg, ok := tcellKeyMsg(tcell.NewEventKey(test.key, 0, tcell.ModNone))
				if !ok || msg.Type != test.want {
					t.Fatalf("traducción de %s inesperada: ok=%v msg=%#v", test.name, ok, msg)
				}
			})
		}
	})

	t.Run("space preserves rune", func(t *testing.T) {
		msg, ok := tcellKeyMsg(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
		if !ok {
			t.Fatal("el espacio debe traducirse")
		}
		if msg.Type != uikit.KeySpace {
			t.Fatalf("tipo inesperado: %v", msg.Type)
		}
		if got := string(msg.Runes); got != " " {
			t.Fatalf("el espacio debe conservar su rune, obtuvo %q", got)
		}
	})

	t.Run("alt direction", func(t *testing.T) {
		msg, ok := tcellKeyMsg(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModAlt))
		if !ok {
			t.Fatal("Alt+Up debe traducirse")
		}
		if msg.Type != uikit.KeyUp || !msg.Alt {
			t.Fatalf("mensaje inesperado: %#v", msg)
		}
	})
}
