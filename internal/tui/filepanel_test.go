package tui

import (
	"strings"
	"testing"
)

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
	p := &FilePanel{Tool: "create_file"}
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
	p := &FilePanel{Tool: "create_file"}
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

func TestVistaPreviaCreceHastaSuLimite(t *testing.T) {
	s := NewStyles(DefaultTheme())
	p := &FilePanel{Tool: "create_file", Path: "a.txt"}
	medir := func() int {
		return len(splitLines(p.renderBody(s, 60)))
	}

	p.Content = "una\ndos\n"
	if got := medir(); got != 2 {
		t.Fatalf("la vista previa corta debe ocupar solo su contenido: got=%d want=2", got)
	}

	p.Content = ""
	for i := 0; i < 80; i++ {
		p.Content += "linea\n"
	}
	if got := medir(); got != previewLines {
		t.Fatalf("la vista previa larga debe respetar el máximo de %d líneas: got=%d", previewLines, got)
	}

	p.Expanded = true
	if medir() <= previewLines {
		t.Fatalf("expandido debe mostrar todo el contenido")
	}
}

func TestFilePanelMarksExistingCreateAsSkipped(t *testing.T) {
	p := &FilePanel{Tool: "create_file", Path: "styles.css"}
	p.Finish("FILE_EXISTS: styles.css already exists. Use str_replace or apply_diff.")
	if !p.Done || p.Failed || !p.Skipped {
		t.Fatalf("FILE_EXISTS should be a recoverable skipped panel: %+v", p)
	}
	if got := p.title(); !strings.Contains(got, "[skipped]") {
		t.Fatalf("expected skipped title, got %q", got)
	}
}

func TestFilePanelRendersNativeWriteFile(t *testing.T) {
	if !IsFileTool("write_file") {
		t.Fatal("write_file tool calls must render as file panels")
	}
	p := &FilePanel{Tool: "write_file", Path: "report.txt"}
	p.Update(`{"path":"report.txt","content":"hola"}`)
	if p.Content != "hola" {
		t.Fatalf("write_file panel should parse content: %+v", p)
	}
}

func TestFilePanelRendersMultiEditArguments(t *testing.T) {
	p := &FilePanel{Tool: "str_replace"}
	p.Update(`{"path":"styles.css","edits":[{"old":"alpha","new":"ALPHA"},{"old":"beta","new":"BETA"}]}`)
	if p.Path != "styles.css" || len(p.Edits) != 2 {
		t.Fatalf("expected two parsed edits, got %+v", p)
	}
	add, del := p.stats()
	if add != 2 || del != 2 {
		t.Fatalf("unexpected multi-edit stats: +%d -%d", add, del)
	}
	body := p.renderBody(NewStyles(DefaultTheme()), 80)
	if !strings.Contains(body, "ALPHA") || !strings.Contains(body, "BETA") {
		t.Fatalf("multi-edit preview omitted changes: %q", body)
	}
}

func TestFilePanelAcceptsStringifiedMultiEdits(t *testing.T) {
	p := &FilePanel{Tool: "str_replace"}
	p.Update(`{"path":"a.txt","edits":"[{\\"oldText\\":\\"one\\",\\"newText\\":\\"ONE\\"}]"}`)
	if len(p.Edits) != 1 || p.Edits[0].Old != "one" || p.Edits[0].New != "ONE" {
		t.Fatalf("stringified edits not rendered: %+v", p.Edits)
	}
}

func TestFilePanelRendersAppendFileAndOverwriteSkip(t *testing.T) {
	if !IsFileTool("append_file") {
		t.Fatal("append_file must render as a file panel")
	}
	p := &FilePanel{Tool: "append_file"}
	p.Update(`{"path":"report.md","content":"## section\nbody\n"}`)
	if p.Path != "report.md" || !strings.Contains(p.Content, "section") {
		t.Fatalf("append panel did not parse streamed content: %+v", p)
	}
	p.Finish("OVERWRITE_REQUIRED: report.md already exists")
	if !p.Skipped || p.Failed {
		t.Fatalf("overwrite requirement should be a skipped panel: %+v", p)
	}
}
