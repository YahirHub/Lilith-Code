package tui

import "testing"

func TestPartialJSONStringOnIncompleteArgs(t *testing.T) {
	raw := `{"path":"index.html","content":"<!DOCTYPE html>\n<html`
	path, ok := partialJSONString(raw, "path")
	if !ok || path != "index.html" {
		t.Fatalf("path inesperado: %q ok=%v", path, ok)
	}
	content, ok := partialJSONString(raw, "content")
	if !ok || content != "<!DOCTYPE html>\n<html" {
		t.Fatalf("contenido parcial inesperado: %q ok=%v", content, ok)
	}
}

func TestFilePanelUpdateTracksLiveContent(t *testing.T) {
	p := &FilePanel{Tool: "write_file"}
	p.Update(`{"path":"a.txt","content":"uno\nd`)
	if p.Path != "a.txt" || p.Content != "uno\nd" {
		t.Fatalf("panel no refleja el stream: %+v", p)
	}
	p.Update(`{"path":"a.txt","content":"uno\ndos\n"}`)
	add, del := p.stats()
	if add != 2 || del != 0 {
		t.Fatalf("conteo inesperado: +%d -%d", add, del)
	}
}

func TestDiffLinesMarksAddedAndRemoved(t *testing.T) {
	d := diffLines([]string{"a", "b", "c"}, []string{"a", "x", "c"})
	var add, del, ctx int
	for _, l := range d {
		switch l.op {
		case '+':
			add++
		case '-':
			del++
		default:
			ctx++
		}
	}
	if add != 1 || del != 1 || ctx != 2 {
		t.Fatalf("diff inesperado: +%d -%d ctx%d", add, del, ctx)
	}
}

func TestFinishNoPliegaLasCreacionesLargas(t *testing.T) {
	p := &FilePanel{Tool: "write_file"}
	long := ""
	for i := 0; i < 40; i++ {
		long += "linea\n"
	}
	p.Content = long
	p.Finish("Escrito a.txt (1 bytes).")
	if p.Expanded || p.Failed {
		t.Fatalf("debería quedar en vista previa sin error: %+v", p)
	}
}

func TestVistaPreviaTieneAlturaFija(t *testing.T) {
	s := NewStyles(DefaultTheme())
	p := &FilePanel{Tool: "write_file", Path: "a.txt"}
	medir := func() int {
		return len(splitLines(p.renderBody(s, 60)))
	}
	p.Content = "una\ndos\n"
	corto := medir()
	for i := 0; i < 80; i++ {
		p.Content += "linea\n"
	}
	if largo := medir(); largo != corto || largo != previewLines {
		t.Fatalf("la vista previa debe medir %d líneas siempre: %d vs %d", previewLines, corto, largo)
	}
	p.Expanded = true
	if medir() <= previewLines {
		t.Fatalf("expandido debe mostrar todo el contenido")
	}
}
