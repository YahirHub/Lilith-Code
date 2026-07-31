package viewport

import "testing"

func TestSetLineSegmentsPreservesOrderAndScrolling(t *testing.T) {
	m := New(80, 3)
	m.SetLineSegments(
		[]string{"cabecera", "historial"},
		[]string{"", "respuesta"},
	)

	if got, want := m.TotalLineCount(), 4; got != want {
		t.Fatalf("conteo inesperado: got=%d want=%d", got, want)
	}
	m.GotoBottom()
	if got, want := m.View(), "historial\n\nrespuesta"; got != want {
		t.Fatalf("vista al fondo inesperada: got=%q want=%q", got, want)
	}
	m.LineUp(1)
	if got, want := m.View(), "cabecera\nhistorial\n"; got != want {
		t.Fatalf("vista desplazada inesperada: got=%q want=%q", got, want)
	}
}

func TestSetContentRemainsCompatibleWithSingleSegment(t *testing.T) {
	m := New(80, 2)
	m.SetContent("uno\ndos\ntres")
	m.GotoBottom()
	if got, want := m.View(), "dos\ntres"; got != want {
		t.Fatalf("SetContent cambió de comportamiento: got=%q want=%q", got, want)
	}
}
