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
	for _, want := range []string{"create_file", "tool_search"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("falta %s en %v", want, got)
		}
	}
	if len(got) >= len(Names()) {
		t.Fatalf("la selección perezosa no debe cargar el catálogo completo: %v", got)
	}
}

func TestEditPromptDoesNotExposeCreateFile(t *testing.T) {
	got := Select("modifica el archivo node-ocr/src/renderer/styles.css y crea un nuevo diseño visual")
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "create_file") {
		t.Fatalf("an existing-file edit prompt must not expose create_file: %v", got)
	}
	for _, want := range []string{"str_replace", "apply_diff", "read_files", "tool_search"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in edit surface %v", want, got)
		}
	}
}

func TestPublicToolCatalogExposesNativeFileWriters(t *testing.T) {
	joined := strings.Join(Names(), ",")
	for _, want := range []string{"create_file", "write_file", "append_file"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s should be public: %v", want, Names())
		}
	}
	if strings.Contains(joined, ",write,") || strings.HasPrefix(joined, "write,") || strings.HasSuffix(joined, ",write") {
		t.Fatalf("ambiguous legacy write must stay hidden: %v", Names())
	}
}

func TestWriteReadAndReplace(t *testing.T) {
	root := t.TempDir()
	env := Env{Root: root}
	ctx := context.Background()

	if _, err := Execute(ctx, "create_file", map[string]any{
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

func TestPlaceholderPathsNeverCreateFiles(t *testing.T) {
	root := t.TempDir()
	env := Env{Root: root}
	for _, path := range []string{"null", "./null", `.\null`, "NULL", "undefined", "nil", "<nil>", "(null)"} {
		_, err := Execute(context.Background(), "create_file", map[string]any{
			"path": path, "content": "must not be written",
		}, env)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "placeholder path") {
			t.Fatalf("path %q should be rejected explicitly, err=%v", path, err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("placeholder calls created project files: %v", entries)
	}
}

func TestPreflightCreateFileDetectsExistingWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, exists, err := PreflightCreateFile(root, "existing.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || !strings.HasPrefix(result, "FILE_EXISTS:") {
		t.Fatalf("existing target should be detected: exists=%v result=%q", exists, result)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("preflight must not modify the target: %q", got)
	}

	result, exists, err = PreflightCreateFile(root, "missing.txt")
	if err != nil || exists || result != "" {
		t.Fatalf("missing target should pass preflight: exists=%v result=%q err=%v", exists, result, err)
	}
}

func TestCreateFileSkipsExistingTargetWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.html")
	before := []byte("<h1>hola</h1>")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: root}
	out, err := Execute(context.Background(), "create_file", map[string]any{
		"path": "a.html", "content": "nuevo",
	}, env)
	if err != nil {
		t.Fatalf("existing file should be a recoverable skip, got %v", err)
	}
	if !strings.HasPrefix(out, "FILE_EXISTS:") || !strings.Contains(out, "str_replace") {
		t.Fatalf("expected compact actionable FILE_EXISTS result, got %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatalf("create_file must never overwrite an existing target: %q", got)
	}
}

func TestStrReplaceSkipsNoOpInsideBatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: root}
	out, err := Execute(context.Background(), "str_replace", map[string]any{
		"path": "a.txt",
		"edits": []any{
			map[string]any{"old": "alpha", "new": "ALPHA"},
			map[string]any{"old": "beta", "new": "beta"},
			map[string]any{"old": "gamma", "new": "GAMMA"},
		},
	}, env)
	if err != nil {
		t.Fatalf("un no-op no debe abortar el lote: %v", err)
	}
	if !strings.Contains(out, "2 replacements") {
		t.Fatalf("se esperaban 2 reemplazos reales, got %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ALPHA\nbeta\nGAMMA\n" {
		t.Fatalf("contenido inesperado: %q", got)
	}
}

func TestStrReplaceAllNoOpsSucceedsWithoutRewrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	before := []byte("alpha\nbeta\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: root}
	out, err := Execute(context.Background(), "str_replace", map[string]any{
		"path": "a.txt",
		"edits": []any{
			map[string]any{"old": "alpha", "new": "alpha"},
			map[string]any{"old": "beta", "new": "beta"},
		},
	}, env)
	if err != nil {
		t.Fatalf("un lote completamente idempotente debe ser éxito: %v", err)
	}
	if !strings.Contains(out, "no changes needed") {
		t.Fatalf("salida inesperada: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatalf("el archivo no debía cambiar: %q", got)
	}
}

func TestStrReplaceValidatesCurrentFileWithoutPriorRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "str_replace", map[string]any{
		"path": "a.txt",
		"old":  "alpha",
		"new":  "ALPHA",
	}, Env{Root: root})
	if err != nil {
		t.Fatalf("str_replace should validate current disk content itself: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "ALPHA\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestApplyDiffValidatesCurrentFileWithoutPriorRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "apply_diff", map[string]any{
		"path": "a.txt",
		"diff": "@@ -1,1 +1,1 @@\n-alpha\n+ALPHA",
	}, Env{Root: root})
	if err != nil {
		t.Fatalf("apply_diff should validate current disk content itself: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "ALPHA\n" {
		t.Fatalf("unexpected content: %q", got)
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

func TestPromptInfoOnlyIncludesActiveTools(t *testing.T) {
	lines, guidelines := PromptInfo([]string{"str_replace"})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "str_replace: Make precise replacements") {
		t.Fatalf("missing active tool snippet: %q", joined)
	}
	if strings.Contains(joined, "run_terminal_command") {
		t.Fatalf("inactive tool leaked into prompt metadata: %q", joined)
	}
	if len(guidelines) == 0 {
		t.Fatal("expected str_replace prompt guidelines")
	}
}

func TestMemoryWriteRejectsPlaceholderPath(t *testing.T) {
	root := t.TempDir()
	_, err := Execute(context.Background(), "memory_write", map[string]any{
		"path": "null", "content": "must not be written",
	}, Env{MemoryDir: root})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "placeholder path") {
		t.Fatalf("memory placeholder path should be rejected, err=%v", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("memory placeholder created files: %v", entries)
	}
}
