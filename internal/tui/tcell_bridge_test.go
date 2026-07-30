package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gdamore/tcell/v2"
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
	key, ok := msgs[0].(tea.KeyMsg)
	if !ok {
		t.Fatalf("el mensaje debe ser tea.KeyMsg, obtuvo %T", msgs[0])
	}
	if !key.Paste {
		t.Fatal("el mensaje debe conservar Paste=true")
	}
	if got := string(key.Runes); got != "ab\nc" {
		t.Fatalf("contenido pegado inesperado: %q", got)
	}
}

func TestTcellKeyTranslation(t *testing.T) {
	t.Run("control", func(t *testing.T) {
		msg, ok := tcellKeyMsg(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl))
		if !ok {
			t.Fatal("Ctrl+C debe traducirse")
		}
		if msg.Type != tea.KeyCtrlC {
			t.Fatalf("tipo inesperado: %v", msg.Type)
		}
	})

	t.Run("alt direction", func(t *testing.T) {
		msg, ok := tcellKeyMsg(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModAlt))
		if !ok {
			t.Fatal("Alt+Up debe traducirse")
		}
		if msg.Type != tea.KeyUp || !msg.Alt {
			t.Fatalf("mensaje inesperado: %#v", msg)
		}
	})
}

func TestRenderTCellClearsContractedRows(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("inicializar pantalla simulada: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(24, 5)

	previous := renderTCell(screen, "uno\ndos\ntres", 0, true)
	if previous != 3 {
		t.Fatalf("altura inicial inesperada: %d", previous)
	}
	current := renderTCell(screen, "nuevo", previous, false)
	if current != 1 {
		t.Fatalf("altura contraída inesperada: %d", current)
	}

	cells, width, height := screen.GetContents()
	for y := 1; y < height; y++ {
		var row strings.Builder
		for x := 0; x < width; x++ {
			cell := cells[y*width+x]
			if len(cell.Runes) == 0 {
				row.WriteRune(' ')
				continue
			}
			row.WriteRune(cell.Runes[0])
		}
		if strings.TrimSpace(row.String()) != "" {
			t.Fatalf("la fila %d conserva contenido fantasma: %q", y, row.String())
		}
	}
}

func TestPublishLatestFramePreservesForcedSync(t *testing.T) {
	frames := make(chan terminalFrame, 1)
	publishLatestFrame(frames, terminalFrame{view: "antes", forceSync: true})
	publishLatestFrame(frames, terminalFrame{view: "después"})

	frame := <-frames
	if frame.view != "después" {
		t.Fatalf("se esperaba el frame más reciente, obtuvo %q", frame.view)
	}
	if !frame.forceSync {
		t.Fatal("el frame reciente debe conservar la sincronización pendiente")
	}
}

func TestFrameNeedsSyncAfterPasteOrEnter(t *testing.T) {
	if !frameNeedsSync(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("texto"), Paste: true}) {
		t.Fatal("un pegado debe solicitar sincronización física")
	}
	if !frameNeedsSync(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("Enter debe solicitar sincronización física")
	}
	if frameNeedsSync(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) {
		t.Fatal("una tecla ordinaria no debe forzar sincronización física")
	}
}
