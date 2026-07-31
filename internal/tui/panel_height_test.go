package tui

import (
	"fmt"
	"strings"
	"testing"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

func TestThinkingPanelCreceHastaSuLimite(t *testing.T) {
	s := NewStyles(DefaultTheme())
	p := &ThinkingPanel{Expanded: true, Content: "uno\ndos"}

	shortHeight := tuistyle.Height(p.View(s, 80, false))
	// borde superior + cabecera + 2 líneas + borde inferior
	if shortHeight != 5 {
		t.Fatalf("panel de razonamiento corto debe ajustarse al contenido: got=%d want=5", shortHeight)
	}

	var long strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&long, "linea %d\n", i)
	}
	p.Content = long.String()
	longHeight := tuistyle.Height(p.View(s, 80, false))
	wantMax := thinkingPreviewLines + 3 // 2 bordes + cabecera
	if longHeight != wantMax {
		t.Fatalf("panel de razonamiento largo debe respetar el máximo: got=%d want=%d", longHeight, wantMax)
	}
}

func TestCommandPanelOutputCreceHastaSuLimite(t *testing.T) {
	s := NewStyles(DefaultTheme())
	p := &CommandPanel{Stdout: "uno\ndos"}

	if got := len(splitLines(p.renderOutput(s, 80))); got != 2 {
		t.Fatalf("salida corta debe ocupar solo dos líneas: got=%d", got)
	}

	var long strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&long, "linea %d\n", i)
	}
	p.Stdout = long.String()
	if got := len(splitLines(p.renderOutput(s, 80))); got != cmdPreviewLines {
		t.Fatalf("salida larga debe respetar el máximo de %d líneas: got=%d", cmdPreviewLines, got)
	}

	p.Expanded = true
	if got := len(splitLines(p.renderOutput(s, 80))); got <= cmdPreviewLines {
		t.Fatalf("modo expandido debe mostrar todo el output: got=%d", got)
	}
}

func TestCappedTailPreviewReservaFilaParaAvisoSinSuperarMaximo(t *testing.T) {
	lines := make([]string, 13)
	for i := range lines {
		lines[i] = fmt.Sprintf("linea %d", i+1)
	}
	view, hidden := cappedTailPreview(lines, 12)
	if hidden != 2 {
		t.Fatalf("líneas ocultas incorrectas: got=%d want=2", hidden)
	}
	if len(view)+1 != 12 {
		t.Fatalf("vista + aviso debe conservar máximo de 12 filas: got=%d", len(view)+1)
	}
	if view[0] != "linea 3" || view[len(view)-1] != "linea 13" {
		t.Fatalf("debe conservar las líneas más recientes: first=%q last=%q", view[0], view[len(view)-1])
	}
}
