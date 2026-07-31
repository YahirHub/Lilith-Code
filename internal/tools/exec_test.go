package tools

import (
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

func TestTerminalCommandSchemaDocumentsOptionalTimeout(t *testing.T) {
	def, ok := Get("run_terminal_command")
	if !ok {
		t.Fatal("run_terminal_command no está registrado")
	}
	if !strings.Contains(def.Description, "when omitted the command runs until it finishes") {
		t.Fatalf("la descripción no documenta la ejecución sin límite: %q", def.Description)
	}
	if strings.Contains(strings.ToLower(def.Description), "default 30") {
		t.Fatalf("la descripción aún anuncia un timeout por defecto: %q", def.Description)
	}
}
