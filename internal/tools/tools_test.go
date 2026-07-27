package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectLazySurface(t *testing.T) {
	if got := Select("hola"); len(got) != 0 {
		t.Fatalf("un saludo no debe cargar esquemas, got %v", got)
	}
	got := Select("crea un archivo index.html con una web de gatitos")
	joined := strings.Join(got, ",")
	for _, want := range []string{"write_file", "tool_search"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("falta %s en %v", want, got)
		}
	}
	if len(got) >= len(Names()) {
		t.Fatalf("la selección perezosa no debe cargar el catálogo completo: %v", got)
	}
}

func TestWriteReadAndReplace(t *testing.T) {
	root := t.TempDir()
	env := Env{Root: root}
	ctx := context.Background()

	if _, err := Execute(ctx, "write_file", map[string]any{
		"path": "web/index.html", "content": "<h1>gatitos</h1>",
	}, env); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "web", "index.html"))
	if err != nil || string(data) != "<h1>gatitos</h1>" {
		t.Fatalf("archivo no escrito: %v %q", err, data)
	}

	out, err := Execute(ctx, "read_files", map[string]any{"paths": []any{"web/index.html"}}, env)
	if err != nil || !strings.Contains(out, "gatitos") {
		t.Fatalf("lectura fallida: %v %q", err, out)
	}

	if _, err := Execute(ctx, "str_replace", map[string]any{
		"path": "web/index.html", "old": "gatitos", "new": "michis",
	}, env); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "web", "index.html"))
	if string(data) != "<h1>michis</h1>" {
		t.Fatalf("reemplazo fallido: %q", data)
	}
}

func TestWriteFileRefusesBlindOverwrite(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.html"), []byte("<h1>hola</h1>"), 0o644)
	env := Env{Root: root, Seen: map[string]bool{}}
	if _, err := Execute(context.Background(), "write_file", map[string]any{
		"path": "a.html", "content": "nuevo",
	}, env); err == nil {
		t.Fatal("se esperaba rechazo de reescritura a ciegas")
	}
	// Tras leerlo, la reescritura explícita sí se permite.
	if _, err := Execute(context.Background(), "read_files", map[string]any{"paths": []any{"a.html"}}, env); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), "write_file", map[string]any{
		"path": "a.html", "content": "nuevo",
	}, env); err != nil {
		t.Fatalf("tras leer debería permitirse: %v", err)
	}
}

// Path escape used to be rejected, but skills stored under ~/.li/skills need
// to read their own assets/scripts via absolute paths. The tool surface now
// trusts the caller (skills are user-authored) so we no longer enforce a
// project-boundary check here.



func TestGlobDoubleStar(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(root, "a", "b", "x.go"), []byte("package a"), 0o644)
	os.WriteFile(filepath.Join(root, "a", "y.txt"), []byte("t"), 0o644)
	out, err := Execute(context.Background(), "glob", map[string]any{"pattern": "**/*.go"}, Env{Root: root})
	if err != nil || !strings.Contains(out, "a/b/x.go") || strings.Contains(out, "y.txt") {
		t.Fatalf("glob incorrecto: %v %q", err, out)
	}
}

func TestToolSearchMaterializes(t *testing.T) {
	var added []string
	env := Env{Root: t.TempDir(), Materialize: func(n []string) { added = n }}
	out, err := Execute(context.Background(), "tool_search", map[string]any{"query": "ejecutar comando"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "run_terminal_command") || len(added) == 0 {
		t.Fatalf("tool_search no activó herramientas: %q %v", out, added)
	}
}
