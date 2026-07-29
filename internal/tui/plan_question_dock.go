package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
)

type planQuestionHit struct {
	row    int
	choice int
}

type planQuestionDock struct {
	open     bool
	selected int
	editing  bool
	input    textinput.Model
}

func newPlanQuestionDock(ctx *AppContext) planQuestionDock {
	ti := textinput.New()
	ti.Prompt = "respuesta › "
	ti.Placeholder = "Escribe tu respuesta"
	ti.CharLimit = 4096
	if ctx != nil {
		ti.PromptStyle = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Secondary).Bold(true)
		ti.TextStyle = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Foreground)
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Muted)
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Secondary)
	}
	return planQuestionDock{input: ti}
}

func (d *planQuestionDock) resetPresentation() {
	d.open = false
	d.selected = 0
	d.editing = false
	d.input.Blur()
	d.input.SetValue("")
}

func (d *planQuestionDock) openPending() tea.Cmd {
	d.open = true
	d.selected = 0
	d.editing = false
	d.input.Blur()
	d.input.SetValue("")
	return nil
}

func (d *planQuestionDock) beginCustom() tea.Cmd {
	d.editing = true
	d.input.SetValue("")
	d.input.CursorEnd()
	return d.input.Focus()
}

func (d *planQuestionDock) current(plans *planstate.Manager) (planstate.State, int, planstate.Question, bool) {
	if plans == nil {
		return planstate.State{}, -1, planstate.Question{}, false
	}
	state := plans.Snapshot()
	idx := planstate.PendingQuestionIndex(state)
	if idx < 0 || idx >= len(state.Questions) {
		return state, -1, planstate.Question{}, false
	}
	return state, idx, state.Questions[idx], true
}

func (d *planQuestionDock) choiceCount(plans *planstate.Manager) int {
	_, _, q, ok := d.current(plans)
	if !ok {
		return 0
	}
	return len(q.Options) + 1 // always include custom answer
}

func (d *planQuestionDock) normalize(plans *planstate.Manager) {
	count := d.choiceCount(plans)
	if count <= 0 {
		d.selected = 0
		return
	}
	if d.selected < 0 {
		d.selected = 0
	}
	if d.selected >= count {
		d.selected = count - 1
	}
}

func (d *planQuestionDock) move(plans *planstate.Manager, delta int) {
	count := d.choiceCount(plans)
	if count <= 0 {
		return
	}
	d.selected = (d.selected + delta + count) % count
}

func compactQuestionWrap(text string, width, maxLines int) []string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return nil
	}
	if width < 12 {
		width = 12
	}
	if maxLines < 1 {
		maxLines = 1
	}
	words := strings.Fields(text)
	lines := make([]string, 0, maxLines)
	line := ""
	for _, word := range words {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
			if len(lines) == maxLines {
				break
			}
		}
		line = word
	}
	if len(lines) < maxLines && line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{truncateOneLine(text, width)}
	}
	if strings.Join(lines, " ") != text {
		last := len(lines) - 1
		lines[last] = truncateOneLine(lines[last]+" …", width)
	}
	return lines
}

func questionVisibleRange(total, selected, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if limit <= 0 || total <= limit {
		return 0, total
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func (m *ChatModel) hasPendingPlanQuestions() bool {
	if m == nil || m.plans == nil {
		return false
	}
	return planstate.PendingQuestionIndex(m.plans.Snapshot()) >= 0
}

func (m *ChatModel) planQuestionLauncherView(w int) string {
	if !m.hasPendingPlanQuestions() || m.planQuestion.open {
		return ""
	}
	state := m.plans.Snapshot()
	remaining := 0
	for _, q := range state.Questions {
		if strings.TrimSpace(state.QuestionAnswers[q.ID]) == "" {
			remaining++
		}
	}
	if remaining == 0 {
		return ""
	}
	accent := lipgloss.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true)
	muted := m.ctx.Styles.Muted
	detailWidth := w - 2
	if detailWidth < 8 {
		detailWidth = 8
	}
	detail := truncateOneLine(fmt.Sprintf("%d decisión(es) del plan pendiente(s) · clic o ? para responder", remaining), detailWidth)
	return accent.Render("?") + muted.Render("  "+detail)
}

func (m *ChatModel) planQuestionDockLayout(w int) (string, []planQuestionHit) {
	if !m.hasPendingPlanQuestions() || !m.planQuestion.open {
		return "", nil
	}
	state, idx, q, ok := m.planQuestion.current(m.plans)
	if !ok {
		return "", nil
	}
	m.planQuestion.normalize(m.plans)
	if w <= 0 {
		w = 80
	}
	contentWidth := w - 2
	if contentWidth < 18 {
		contentWidth = 18
	}

	theme := m.ctx.Styles.Theme
	accent := lipgloss.NewStyle().Foreground(theme.Secondary).Bold(true)
	strong := lipgloss.NewStyle().Foreground(theme.Foreground).Bold(true)
	plain := lipgloss.NewStyle().Foreground(theme.Foreground)
	muted := m.ctx.Styles.Muted

	lines := []string{accent.Render("?") + muted.Render(fmt.Sprintf("  Plan · pregunta %d/%d", idx+1, len(state.Questions)))}
	questionLines := 2
	if m.ctx != nil && m.ctx.Height > 0 && m.ctx.Height <= 16 {
		questionLines = 1
	}
	for _, line := range compactQuestionWrap(q.Question, contentWidth-2, questionLines) {
		lines = append(lines, "  "+strong.Render(line))
	}

	hits := make([]planQuestionHit, 0, 4)
	if m.planQuestion.editing {
		inputWidth := contentWidth - 2 - lipgloss.Width(m.planQuestion.input.Prompt)
		m.planQuestion.input.Width = maxInt(6, inputWidth)
		lines = append(lines, "  "+m.planQuestion.input.View())
		lines = append(lines, muted.Render("  Enter guardar · Esc cancelar"))
		return strings.Join(lines, "\n"), hits
	}

	total := len(q.Options) + 1
	limit := 4
	if m.ctx != nil && m.ctx.Height > 0 {
		switch {
		case m.ctx.Height <= 12:
			limit = 2
		case m.ctx.Height <= 18:
			limit = 3
		}
	}
	start, end := questionVisibleRange(total, m.planQuestion.selected, limit)
	for choice := start; choice < end; choice++ {
		row := len(lines)
		selected := choice == m.planQuestion.selected
		prefix := "  "
		style := plain
		if selected {
			prefix = "› "
			style = accent
		}
		var label string
		if choice < len(q.Options) {
			label = fmt.Sprintf("%d. %s", choice+1, q.Options[choice].Label)
			if desc := strings.TrimSpace(q.Options[choice].Description); desc != "" {
				label += " — " + desc
			}
		} else {
			label = fmt.Sprintf("%d. Otra respuesta", choice+1)
		}
		label = truncateOneLine(label, maxInt(8, contentWidth-2))
		lines = append(lines, prefix+style.Render(label))
		hits = append(hits, planQuestionHit{row: row, choice: choice})
	}
	lines = append(lines, muted.Render(fmt.Sprintf("  ↑↓ elegir · Enter responder · Esc volver · %d/%d", m.planQuestion.selected+1, total)))
	return strings.Join(lines, "\n"), hits
}

func (m *ChatModel) planQuestionDockView(w int) string {
	view, _ := m.planQuestionDockLayout(w)
	return view
}

func (m *ChatModel) openPlanQuestions() tea.Cmd {
	if !m.hasPendingPlanQuestions() {
		return nil
	}
	m.returnToInteractionBottom()
	m.planQuestion.openPending()
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return tea.EnableMouseCellMotion
}

func (m *ChatModel) closePlanQuestions() tea.Cmd {
	m.planQuestion.open = false
	m.planQuestion.editing = false
	m.planQuestion.input.Blur()
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	// Keep mouse reporting while a pending launcher is visible so it remains
	// clickable. Once the request is answered/superseded, normal chat releases it.
	if m.hasPendingPlanQuestions() {
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

func (m *ChatModel) answerCurrentPlanQuestion(value string) tea.Cmd {
	_, _, q, ok := m.planQuestion.current(m.plans)
	if !ok {
		m.planQuestion.resetPresentation()
		return tea.DisableMouse
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	updated, complete, err := m.plans.AnswerQuestion(q.ID, value)
	if err != nil {
		m.AddError(err.Error())
		return nil
	}
	m.planQuestion.selected = 0
	m.planQuestion.editing = false
	m.planQuestion.input.Blur()
	m.planQuestion.input.SetValue("")
	if !complete {
		m.persist()
		if m.ctx.Width > 0 && m.ctx.Height > 0 {
			m.Resize(m.ctx.Width, m.ctx.Height)
		}
		return nil
	}
	answers := planstate.FormatQuestionAnswers(updated)
	m.planQuestion.resetPresentation()
	if answers == "" {
		m.AddError("No se pudieron preparar las respuestas del plan.")
		return tea.DisableMouse
	}
	return tea.Batch(m.startPlanQuestionContinuation(answers), tea.DisableMouse)
}

func (m *ChatModel) selectCurrentPlanQuestionChoice() tea.Cmd {
	_, _, q, ok := m.planQuestion.current(m.plans)
	if !ok {
		return m.closePlanQuestions()
	}
	if m.planQuestion.selected >= 0 && m.planQuestion.selected < len(q.Options) {
		return m.answerCurrentPlanQuestion(q.Options[m.planQuestion.selected].Label)
	}
	return m.planQuestion.beginCustom()
}

func (m *ChatModel) handlePlanQuestionKey(v tea.KeyMsg) (bool, tea.Cmd) {
	if !m.hasPendingPlanQuestions() {
		return false, nil
	}
	key := v.String()
	// Page/half-page navigation always belongs to the transcript, even while a
	// question is open. Once the viewport leaves the bottom the whole question
	// dock scrolls out with the editor and comes back unchanged at End/bottom.
	if m.planQuestion.open && isScrollKey(key) {
		return false, nil
	}
	if m.planQuestion.open && m.userScrolled {
		m.returnToInteractionBottom()
	}
	if !m.planQuestion.open {
		if key == "?" {
			return true, m.openPlanQuestions()
		}
		return false, nil
	}

	if m.planQuestion.editing {
		switch key {
		case "esc":
			m.planQuestion.editing = false
			m.planQuestion.input.Blur()
			return true, nil
		case "enter":
			return true, m.answerCurrentPlanQuestion(m.planQuestion.input.Value())
		}
		var cmd tea.Cmd
		m.planQuestion.input, cmd = m.planQuestion.input.Update(v)
		return true, cmd
	}

	switch key {
	case "esc":
		return true, m.closePlanQuestions()
	case "up", "k", "shift+tab":
		m.planQuestion.move(m.plans, -1)
		return true, nil
	case "down", "j", "tab":
		m.planQuestion.move(m.plans, 1)
		return true, nil
	case "enter", " ":
		return true, m.selectCurrentPlanQuestionChoice()
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if idx < m.planQuestion.choiceCount(m.plans) {
			m.planQuestion.selected = idx
			return true, m.selectCurrentPlanQuestionChoice()
		}
	}
	return true, nil
}

func (m *ChatModel) planQuestionChromeY() int {
	w, h := m.ctx.Width, m.ctx.Height
	if w <= 0 {
		w = 80
	}
	used, maxCtx := m.contextUsage()
	return m.viewportHeightForFrame(w, h, used, maxCtx)
}

func (m *ChatModel) handlePlanQuestionMouse(v tea.MouseMsg) (bool, tea.Cmd) {
	if !m.hasPendingPlanQuestions() {
		return false, nil
	}
	e, ok := mouseLeftPress(v)
	if !ok {
		return false, nil
	}
	startY := m.planQuestionChromeY()
	if !m.planQuestion.open {
		if e.Y == startY {
			return true, m.openPlanQuestions()
		}
		return false, nil
	}
	_, hits := m.planQuestionDockLayout(m.ctx.Width)
	rel := e.Y - startY
	for _, hit := range hits {
		if hit.row != rel {
			continue
		}
		m.planQuestion.selected = hit.choice
		return true, m.selectCurrentPlanQuestionChoice()
	}
	return false, nil
}

// startPlanQuestionContinuation resumes the Plan turn after the interactive
// request is fully answered without changing the primary agent selected for
// the NEXT ordinary user turn. This mirrors OpenCode's question reply channel:
// answers continue the request that asked them, even if Tab already selected
// Build for what comes afterwards.
func (m *ChatModel) startPlanQuestionContinuation(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	m.appendHistory(openai.Message{Role: "user", Content: text})
	m.activeTools = m.selectToolsForPrompt(text, planstate.Plan)
	m.toolSteps = 0
	m.toolFallback = ""
	if err := m.beginTurnMode(planstate.Plan); err != nil {
		m.AddError(err.Error())
		return nil
	}
	m.persist()
	m.refreshTranscript(true)
	return m.runTurn()
}
