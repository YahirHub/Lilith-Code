package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	litodo "github.com/lilith/li/internal/todo"
)

const todoWidgetTaskLimit = 3

func (m *ChatModel) todoRevision() int {
	if m == nil || m.todos == nil {
		return 0
	}
	return m.todos.Snapshot().Revision
}

func (m *ChatModel) todoStatePointer() *litodo.State {
	if m == nil || m.todos == nil {
		return nil
	}
	state := m.todos.Snapshot()
	if len(state.Tasks) == 0 {
		return nil
	}
	return &state
}

func (m *ChatModel) todoBlock() string {
	if m == nil || m.todos == nil {
		return ""
	}
	return litodo.PromptBlock(m.todos.Snapshot())
}

// cleanupCompletedTodos mirrors Pi's next-turn cleanup. It runs only when a
// fresh user turn is about to start, never between tool continuations in the
// same turn, so a just-completed plan remains visible through the current work.
func (m *ChatModel) cleanupCompletedTodos() bool {
	if m == nil || m.todos == nil {
		return false
	}
	_, changed, err := m.todos.CleanupCompleted()
	if err != nil || !changed {
		return false
	}
	m.invalidateContextUsage()
	if m.ctx != nil && m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return true
}

// todoDisplayTask keeps dependency keys out of the user-facing widget. Tasks
// participating in the dependency graph get small plan-order numbers instead.
type todoDisplayTask struct {
	Task       litodo.Task
	Number     int
	DepNumbers []int
}

func todoDisplayTasks(tasks []litodo.Task) []todoDisplayTask {
	participating := map[string]bool{}
	for _, task := range tasks {
		if len(task.DependsOn) == 0 {
			continue
		}
		participating[task.Key] = true
		for _, dep := range task.DependsOn {
			participating[dep] = true
		}
	}
	numbers := map[string]int{}
	for _, task := range tasks {
		if participating[task.Key] {
			numbers[task.Key] = len(numbers) + 1
		}
	}
	out := make([]todoDisplayTask, 0, len(tasks))
	for _, task := range tasks {
		row := todoDisplayTask{Task: task, Number: numbers[task.Key]}
		for _, dep := range task.DependsOn {
			if n := numbers[dep]; n > 0 {
				row.DepNumbers = append(row.DepNumbers, n)
			}
		}
		out = append(out, row)
	}
	return out
}

func collapsedTodoWindow(tasks []todoDisplayTask) []todoDisplayTask {
	if len(tasks) <= todoWidgetTaskLimit {
		return append([]todoDisplayTask(nil), tasks...)
	}
	allDone := true
	for _, task := range tasks {
		if task.Task.Status != litodo.Completed {
			allDone = false
			break
		}
	}
	if allDone {
		return append([]todoDisplayTask(nil), tasks[len(tasks)-todoWidgetTaskLimit:]...)
	}
	active := -1
	for i, task := range tasks {
		if task.Task.Status == litodo.InProgress {
			active = i
			break
		}
	}
	if active < 0 {
		return append([]todoDisplayTask(nil), tasks[:todoWidgetTaskLimit]...)
	}
	start := active - 1
	if start < 0 {
		start = 0
	}
	maxStart := len(tasks) - todoWidgetTaskLimit
	if start > maxStart {
		start = maxStart
	}
	return append([]todoDisplayTask(nil), tasks[start:start+todoWidgetTaskLimit]...)
}

func renderTodoTask(task todoDisplayTask, width int, s Styles) string {
	marker := "[ ]"
	markerStyle := lipgloss.NewStyle().Foreground(s.Theme.Muted)
	subjectStyle := lipgloss.NewStyle().Foreground(s.Theme.Foreground)
	switch task.Task.Status {
	case litodo.InProgress:
		marker = "[>]"
		markerStyle = lipgloss.NewStyle().Foreground(s.Theme.Warning).Bold(true)
	case litodo.Completed:
		marker = "[x]"
		markerStyle = lipgloss.NewStyle().Foreground(s.Theme.Success)
		subjectStyle = lipgloss.NewStyle().Foreground(s.Theme.Muted).Strikethrough(true)
	}
	relation := ""
	if task.Number > 0 {
		relation = fmt.Sprintf(" #%d", task.Number)
	}
	if len(task.DepNumbers) > 0 {
		refs := make([]string, 0, len(task.DepNumbers))
		for _, n := range task.DepNumbers {
			refs = append(refs, fmt.Sprintf("#%d", n))
		}
		relation += " <- " + strings.Join(refs, ", ")
	}
	prefix := markerStyle.Render(marker) + " "
	plainRelation := relation
	available := width - lipgloss.Width(marker) - 1 - lipgloss.Width(plainRelation)
	if available < 8 {
		available = 8
	}
	subject := truncateOneLine(task.Task.Subject, available)
	return prefix + subjectStyle.Render(subject) + s.Muted.Render(relation)
}

// todoWidgetView is a read-only, fixed plan summary placed above the editor.
// It deliberately uses ASCII status markers instead of emoji so rendering is
// stable across Windows/Linux terminals and follows Lilith's current UI rules.
func (m *ChatModel) todoWidgetView(w int) string {
	if m == nil || m.todos == nil {
		return ""
	}
	state := m.todos.Snapshot()
	if len(state.Tasks) == 0 {
		return ""
	}
	boxWidth := w - 2
	if boxWidth < 12 {
		boxWidth = w
	}
	contentWidth := boxWidth - 2
	if contentWidth < 10 {
		contentWidth = boxWidth
	}
	completed := 0
	for _, task := range state.Tasks {
		if task.Status == litodo.Completed {
			completed++
		}
	}
	lines := []string{
		m.ctx.Styles.Accent.Render(fmt.Sprintf("Tareas %d/%d", completed, len(state.Tasks))) +
			m.ctx.Styles.Muted.Render(fmt.Sprintf(" · rev %d", state.Revision)),
	}
	display := todoDisplayTasks(state.Tasks)
	visible := collapsedTodoWindow(display)
	for _, task := range visible {
		lines = append(lines, renderTodoTask(task, contentWidth, m.ctx.Styles))
	}
	if len(display) > len(visible) {
		lines = append(lines, m.ctx.Styles.Muted.Render(fmt.Sprintf("... %d tarea(s) más", len(display)-len(visible))))
	}
	return lipgloss.NewStyle().Width(boxWidth).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func isTodoToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "todo_write", "todowrite", "write_todos", "todo":
		return true
	default:
		return false
	}
}

func todoCallTaskCount(raw string) int {
	var payload struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return -1
	}
	if payload.Tasks == nil {
		return -1
	}
	return len(payload.Tasks)
}
