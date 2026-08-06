package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalCommandTimeoutOmittedIsUnlimited(t *testing.T) {
	if got := terminalCommandTimeout(map[string]any{"command": "go build ./..."}); got != 0 {
		t.Fatalf("timeout omitido = %s; se esperaba ejecución sin límite", got)
	}
}

func TestTerminalCommandTimeoutExplicitIsPreserved(t *testing.T) {
	if got := terminalCommandTimeout(map[string]any{"timeout_seconds": float64(720)}); got != 12*time.Minute {
		t.Fatalf("timeout explícito = %s; se esperaban 12m", got)
	}
}

func TestTerminalCommandSearchGetsSafetyTimeout(t *testing.T) {
	got := terminalCommandTimeout(map[string]any{"command": `grep -rn "needle"`})
	if got != repositorySearchTimeout {
		t.Fatalf("repository search timeout = %s; want %s", got, repositorySearchTimeout)
	}
}

func TestTerminalCommandExplicitSearchTimeoutWins(t *testing.T) {
	got := terminalCommandTimeout(map[string]any{"command": `grep -rn "needle"`, "timeout_seconds": float64(90)})
	if got != 90*time.Second {
		t.Fatalf("explicit search timeout = %s; want 90s", got)
	}
}

func TestTerminalCommandSchemaDocumentsOptionalTimeout(t *testing.T) {
	def, ok := Get("run_terminal_command")
	if !ok {
		t.Fatal("run_terminal_command no está registrado")
	}
	if !strings.Contains(def.Description, "builds/tests/installations run until completion") {
		t.Fatalf("la descripción no documenta la ejecución sin límite para trabajos largos: %q", def.Description)
	}
	if !strings.Contains(def.Description, "30-second safety deadline") {
		t.Fatalf("la descripción no documenta el límite de búsquedas: %q", def.Description)
	}
	if strings.Contains(strings.ToLower(def.Description), "default 30") {
		t.Fatalf("la descripción aún anuncia un timeout por defecto: %q", def.Description)
	}
}

func TestTerminalCommandSchemaDocumentsShellSelection(t *testing.T) {
	def, ok := Get("run_terminal_command")
	if !ok {
		t.Fatal("run_terminal_command no está registrado")
	}
	if !strings.Contains(def.Description, "PowerShell") || !strings.Contains(def.Description, "CMD syntax") || !strings.Contains(def.Description, "Bash") {
		t.Fatalf("shell selection is not documented: %q", def.Description)
	}
	params, ok := def.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("parameters type=%T", def.Parameters)
	}
	properties, _ := params["properties"].(map[string]any)
	if _, ok := properties["shell"]; !ok {
		t.Fatalf("shell parameter missing: %#v", properties)
	}
}

func TestTerminalCommandSchemaIncludesPortableShell(t *testing.T) {
	def, ok := Get("run_terminal_command")
	if !ok {
		t.Fatal("run_terminal_command no está registrado")
	}
	params := def.Parameters.(map[string]any)
	properties := params["properties"].(map[string]any)
	shellParam := properties["shell"].(map[string]any)
	enum := shellParam["enum"].([]string)
	found := false
	for _, item := range enum {
		if item == "portable" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("portable missing from shell enum: %#v", enum)
	}
	if !strings.Contains(def.Description, "embedded pure-Go") || !strings.Contains(def.Description, "does not emulate a full Linux userland") {
		t.Fatalf("portable shell limits not documented: %q", def.Description)
	}
}

func TestNativeCodeSearchWorksWithoutRipgrep(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "sample.go"), []byte("package sample\n// PortableNeedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runNativeCodeSearch(context.Background(), map[string]any{
		"pattern": "PortableNeedle",
		"literal": true,
		"glob":    "*.go",
	}, Env{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "internal/sample.go:2:// PortableNeedle") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCodeSearchDescriptionDocumentsGoFallback(t *testing.T) {
	def, ok := Get("code_search")
	if !ok {
		t.Fatal("code_search no está registrado")
	}
	if !strings.Contains(def.Description, "pure-Go search engine") || !strings.Contains(def.PromptSnippet, "native Go fallback") {
		t.Fatalf("fallback not documented: %#v", def)
	}
}
