package tools

import (
	"context"
	"strings"
	"testing"

	litodo "github.com/lilith/li/internal/todo"
)

func TestTodoWriteIsAlwaysAvailableForSubstantivePrompts(t *testing.T) {
	got := Select("corrige el bug del selector y ejecuta las pruebas")
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "todo_write") {
		t.Fatalf("todo_write should be present for substantive prompts: %v", got)
	}
	if got := Select("hola"); len(got) != 0 {
		t.Fatalf("direct chat should remain schema-free: %v", got)
	}
}

func TestTodoWriteAtomicUpdatesAndAliases(t *testing.T) {
	manager := litodo.NewManager(nil)
	env := Env{Todos: manager}
	ctx := context.Background()
	out, err := Execute(ctx, "todo_write", map[string]any{
		"baseRevision": float64(0),
		"tasks": []any{
			map[string]any{"key": "inspect", "subject": "Inspect implementation", "status": "in_progress"},
			map[string]any{"key": "verify", "subject": "Run tests", "status": "pending", "dependsOn": []any{"inspect"}},
		},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Todo plan revision 1") {
		t.Fatalf("out=%q", out)
	}

	out, err = Execute(ctx, "write_todos", map[string]any{
		"baseRevision": float64(1),
		"tasks": []any{
			map[string]any{"key": "inspect", "status": "completed"},
			map[string]any{"key": "verify", "status": "in_progress"},
		},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[completed] inspect") || !strings.Contains(out, "[in_progress] verify") {
		t.Fatalf("unexpected sparse alias update: %q", out)
	}
}

func TestTodoWriteRejectsMissingManager(t *testing.T) {
	_, err := Execute(context.Background(), "todo_write", map[string]any{"tasks": []any{}}, Env{})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("err=%v", err)
	}
}
