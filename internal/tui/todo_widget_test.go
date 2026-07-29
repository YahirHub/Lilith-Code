package tui

import (
	"strings"
	"testing"

	litodo "github.com/lilith/li/internal/todo"
)

func todoTestManager(t *testing.T) *litodo.Manager {
	t.Helper()
	m := litodo.NewManager(nil)
	_, err := m.Write(litodo.SnapshotInput{Tasks: []litodo.TaskPatch{
		{Key: "inspect", Subject: stringPtr("Inspect implementation"), Status: todoStatusPtr(litodo.Completed)},
		{Key: "implement", Subject: stringPtr("Implement todo tool"), Status: todoStatusPtr(litodo.InProgress), DependsOn: todoDepsPtr("inspect")},
		{Key: "verify", Subject: stringPtr("Verify behavior"), Status: todoStatusPtr(litodo.Pending), DependsOn: todoDepsPtr("implement")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func stringPtr(v string) *string                   { return &v }
func todoStatusPtr(v litodo.Status) *litodo.Status { return &v }
func todoDepsPtr(v ...string) *[]string            { out := append([]string(nil), v...); return &out }

func TestTodoWidgetRendersPlanAboveInputWithoutEmoji(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme()), Width: 100, Height: 40}
	m := ChatModel{ctx: ctx, todos: todoTestManager(t)}
	view := m.todoWidgetView(100)
	for _, want := range []string{"Tareas 1/3", "[x]", "[>]", "[ ]", "Implement todo tool", "#2 <- #1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in:\n%s", want, view)
		}
	}
	if strings.ContainsAny(view, "✓◐○☐") {
		t.Fatalf("widget should use ASCII markers only:\n%s", view)
	}
}

func TestTodoBlockCarriesExactRevisionIntoSystemPrompt(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := ChatModel{ctx: ctx, todos: todoTestManager(t)}
	prompt := systemPrompt([]string{"todo_write"}, "", m.todoBlock())
	if !strings.Contains(prompt, `<todo_state revision="1">`) {
		t.Fatalf("missing todo checkpoint:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[in_progress] implement: Implement todo tool <- inspect") {
		t.Fatalf("missing exact task state:\n%s", prompt)
	}
	if !strings.Contains(prompt, "todo_write: Maintain the current multi-step task plan") {
		t.Fatalf("missing todo tool prompt snippet:\n%s", prompt)
	}
}

func TestDescribeTodoCallIsCompact(t *testing.T) {
	call := makeToolCall("todo_write", `{"tasks":[{"key":"a"},{"key":"b"}]}`)
	got := describeCall(call)
	if got != "$ todo_write 2 tarea(s)" {
		t.Fatalf("got %q", got)
	}
}
