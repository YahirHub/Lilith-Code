package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	litodo "github.com/lilith/li/internal/todo"
	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
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
	markerStyle := tuistyle.NewStyle().Foreground(s.Theme.Muted)
	subjectStyle := tuistyle.NewStyle().Foreground(s.Theme.Foreground)
	switch task.Task.Status {
	case litodo.InProgress:
		marker = "[>]"
		markerStyle = tuistyle.NewStyle().Foreground(s.Theme.Warning).Bold(true)
	case litodo.Completed:
		marker = "[x]"
		markerStyle = tuistyle.NewStyle().Foreground(s.Theme.Success)
		subjectStyle = tuistyle.NewStyle().Foreground(s.Theme.Muted).Strikethrough(true)
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
	available := width - tuistyle.Width(marker) - 1 - tuistyle.Width(plainRelation)
	if available < 8 {
		available = 8
	}
	subject := truncateOneLine(task.Task.Subject, available)
	return prefix + subjectStyle.Render(subject) + s.Muted.Render(relation)
}

// todoWidgetView renders the current model-owned task plan. Compact mode keeps
// only the active neighborhood visible; expanded mode renders the whole plan.
// The widget itself lives in the bottom interaction flow and therefore scrolls
// away with the editor whenever the user reads older transcript content.
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
	display := todoDisplayTasks(state.Tasks)
	expandable := len(display) > todoWidgetTaskLimit
	modeHint := ""
	if expandable {
		modeHint = " · Ctrl+T"
	}
	lines := []string{
		m.ctx.Styles.Accent.Render(fmt.Sprintf("Tareas %d/%d", completed, len(state.Tasks))) +
			m.ctx.Styles.Muted.Render(fmt.Sprintf(" · rev %d%s", state.Revision, modeHint)),
	}
	visible := display
	if !m.todoExpanded {
		visible = collapsedTodoWindow(display)
	}
	for _, task := range visible {
		lines = append(lines, renderTodoTask(task, contentWidth, m.ctx.Styles))
	}
	if !m.todoExpanded && len(display) > len(visible) {
		lines = append(lines, m.ctx.Styles.Muted.Render(fmt.Sprintf("... %d tarea(s) más", len(display)-len(visible))))
	}
	return tuistyle.NewStyle().Width(boxWidth).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func (m *ChatModel) todoExpandable() bool {
	if m == nil || m.todos == nil || m.effectiveAgentMode() == "plan" {
		return false
	}
	return len(m.todos.Snapshot().Tasks) > todoWidgetTaskLimit
}

func (m *ChatModel) toggleTodoExpanded() bool {
	if !m.todoExpandable() {
		return false
	}
	m.todoExpanded = !m.todoExpanded
	if m.ctx != nil && m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return true
}

// todoChromeBounds returns the Todo rows in terminal coordinates while the
// interaction tail is visible. It deliberately mirrors bottomChromeParts so a
// click can toggle the Todo without introducing a separate layout system.
func (m *ChatModel) todoChromeBounds(w, h int) (int, int, bool) {
	if m == nil || m.ctx == nil || m.userScrolled || !m.todoExpandable() || m.planQuestion.open {
		return 0, 0, false
	}
	if w <= 0 {
		w = 80
	}
	used, maxCtx := m.contextUsage()
	y := m.viewportHeightForFrame(w, h, used, maxCtx)
	if launcher := m.planQuestionLauncherView(w); launcher != "" {
		y += tuistyle.Height(launcher)
	}
	if plan := m.planWidgetView(w); plan != "" {
		y += tuistyle.Height(plan)
	}
	todo := m.todoWidgetView(w)
	if todo == "" {
		return 0, 0, false
	}
	return y, y + tuistyle.Height(todo) - 1, true
}

func (m *ChatModel) handleTodoMouse(v uikit.MouseMsg) (bool, uikit.Cmd) {
	if !m.todoExpandable() || m.userScrolled {
		return false, nil
	}
	e, ok := mouseLeftPress(v)
	if !ok {
		return false, nil
	}
	w, h := m.ctx.Width, m.ctx.Height
	start, end, ok := m.todoChromeBounds(w, h)
	if !ok || e.Y < start || e.Y > end {
		return false, nil
	}
	m.toggleTodoExpanded()
	return true, m.chatMouseModeCmd()
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
