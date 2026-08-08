package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"

	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
)

func (m *ChatModel) goalStatePointer() *ligoal.State {
	if m == nil || m.goals == nil {
		return nil
	}
	return m.goals.Snapshot()
}

func (m *ChatModel) ensureGoalManager() *ligoal.Manager {
	if m.goals == nil {
		m.goals = ligoal.NewManager(nil)
	}
	return m.goals
}

func goalStatusLabel(status ligoal.Status) string {
	switch status {
	case ligoal.Active:
		return "activo"
	case ligoal.Paused:
		return "pausado"
	case ligoal.Blocked:
		return "bloqueado"
	case ligoal.Interrupted:
		return "interrumpido"
	case ligoal.Complete:
		return "completado"
	default:
		return string(status)
	}
}

func formatGoalState(s *ligoal.State) string {
	if s == nil {
		return "No hay un objetivo persistente. Usa /goal <objetivo>."
	}
	result := fmt.Sprintf("Goal · %s\n%s\nTokens usados: %d · Tiempo: %s · Sin límites artificiales", goalStatusLabel(s.Status), s.Objective, s.TokensUsed, (time.Duration(s.TimeUsedSeconds) * time.Second).String())
	if strings.TrimSpace(s.Summary) != "" {
		result += "\nResumen: " + s.Summary
	}
	return result
}

// runGoalCommand implements the user-controlled half of Codex-style durable
// goals. Model-controlled transitions are intentionally narrower and live in
// create_goal/get_goal/update_goal.
func (m *ChatModel) runGoalCommand(args string) uikit.Cmd {
	args = strings.TrimSpace(args)
	mgr := m.ensureGoalManager()
	if args == "" || strings.EqualFold(args, "status") || strings.EqualFold(args, "show") {
		m.AddSystem(formatGoalState(mgr.Snapshot()))
		return nil
	}

	fields := strings.Fields(args)
	if len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "clear", "remove", "delete":
			if mgr.Clear() {
				m.AddSystem("Goal eliminado.")
			} else {
				m.AddSystem("No había un goal configurado.")
			}
			m.persistGoalState()
			return nil
		case "pause", "pausar":
			if err := mgr.UpdateStatus(ligoal.Paused); err != nil {
				m.AddError(err.Error())
			} else {
				m.AddSystem("Goal pausado. Usa /goal resume para continuar.")
				m.persistGoalState()
			}
			return nil
		case "resume", "reanudar", "continue", "continuar":
			if err := mgr.Resume(); err != nil {
				m.AddError(err.Error())
				return nil
			}
			m.setAgentMode(planstate.Build)
			m.AddSystem("Goal reanudado.")
			m.persistGoalState()
			if m.activeTurnID != 0 {
				m.turnGoalManaged = true
				m.enqueue("El goal persistente fue reanudado. Continúa trabajando hacia el objetivo activo.", queueSteer)
				return nil
			}
			return m.startGoalContinuation("Reanuda el goal persistente y continúa trabajando de forma autónoma hasta completarlo o quedar realmente bloqueado.")
		case "complete", "completed", "completar":
			summary := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
			var completeErr error
			if summary == "" {
				completeErr = mgr.UpdateStatus(ligoal.Complete)
			} else {
				completeErr = mgr.Complete(summary)
			}
			if completeErr != nil {
				m.AddError(completeErr.Error())
			} else {
				m.AddSystem("Goal marcado como completado.")
				m.persistGoalState()
			}
			return nil
		}
	}

	objective, deprecatedBudget, err := parseGoalArgs(args)
	if err != nil {
		m.AddError(err.Error())
		return nil
	}
	if _, err := mgr.Set(objective); err != nil {
		m.AddError(err.Error())
		return nil
	}
	// Goal is an input mode, not a separate implementation runtime. Once the
	// objective is durable, every continuation executes with Build tools.
	m.setAgentMode(planstate.Build)
	m.invalidateContextUsage()
	m.persistGoalState()
	if deprecatedBudget {
		m.AddSystem("Los presupuestos de Goal fueron eliminados. El objetivo continuará sin límite artificial de tokens, pasos, turnos ni tiempo.")
	}
	m.AddSystem("Goal activo: " + objective)

	// /goal is available while a task is in progress in current Codex. Updating
	// the durable objective must not create a parallel parent turn; steer the
	// running agent and let the next safe boundary pick up the new goal state.
	if m.activeTurnID != 0 {
		m.turnGoalManaged = true
		m.enqueue("El objetivo persistente cambió a: "+objective+". Ajusta el trabajo actual a este goal.", queueSteer)
		return nil
	}
	return m.startGoalContinuation("Empieza a trabajar ahora en el goal persistente. Continúa autónomamente hasta completarlo o quedar bloqueado por una decisión material del usuario.")
}

func parseGoalArgs(args string) (string, bool, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", false, fmt.Errorf("uso: /goal <objetivo>")
	}
	fields := strings.Fields(args)
	objective := make([]string, 0, len(fields))
	deprecatedBudget := false
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "--tokens" || f == "--token-budget" {
			deprecatedBudget = true
			if i+1 < len(fields) {
				i++
			}
			continue
		}
		objective = append(objective, f)
	}
	text := strings.TrimSpace(strings.Join(objective, " "))
	if text == "" {
		return "", deprecatedBudget, fmt.Errorf("el objetivo de /goal está vacío")
	}
	return text, deprecatedBudget, nil
}

func (m *ChatModel) persistGoalState() {
	m.invalidateContextUsage()
	if m.activeTurnID != 0 {
		m.forceLivePersist()
		return
	}
	m.persist()
}

func (m *ChatModel) startGoalContinuation(instruction string) uikit.Cmd {
	if m.goals == nil || !m.goals.Active() || m.activeTurnID != 0 {
		return nil
	}
	m.appendHistory(openai.Message{Role: "user", Content: "<goal_runtime_instruction>\n" + strings.TrimSpace(instruction) + "\n</goal_runtime_instruction>"})
	mode := planstate.Build
	m.activeTools = m.selectToolsForPrompt(m.goals.Snapshot().Objective+"\n"+instruction, mode)
	if err := m.beginTurnMode(mode); err != nil {
		_ = m.goals.UpdateStatus(ligoal.Interrupted)
		m.AddError(err.Error())
		m.persistGoalState()
		return nil
	}
	if m.turnAgentMode != planstate.Plan {
		m.cleanupCompletedTodos()
	}
	m.persistTurnStart()
	return uikit.Batch(m.runTurn(), m.chatMouseModeCmd())
}

// continueGoalAtBoundary keeps the same logical parent turn alive without a
// visible synthetic user message. It is called only after a complete model
// response or tool boundary and therefore never overlaps provider requests.
func (m *ChatModel) continueGoalAtBoundary() bool {
	if !m.turnGoalManaged || m.goals == nil || !m.goals.Active() || m.activeTurnID == 0 {
		return false
	}
	m.appendHistory(openai.Message{Role: "user", Content: "<goal_continue>Continue working autonomously toward the active durable goal. Do not stop merely to report progress. Finish the objective and call goal_complete with a concise summary, or use update_goal with status=blocked when a material user decision prevents progress.</goal_continue>"})
	m.forceLivePersist()
	m.thinking = true
	m.working = false
	m.assistantActive = -1
	return true
}

func (m *ChatModel) accountGoalRequest() {
	if m.goalRequestTokens <= 0 || m.goals == nil {
		m.goalRequestTokens = 0
		return
	}
	m.goals.AddUsage(m.goalRequestTokens)
	m.goalRequestTokens = 0
	m.invalidateContextUsage()
	m.forceLivePersist()
}

func (m *ChatModel) goalStopsCurrentLoop() bool {
	if m == nil || !m.turnGoalManaged {
		return false
	}
	s := m.goalStatePointer()
	return s == nil || s.Status != ligoal.Active
}

func (m *ChatModel) pauseGoalOnInterrupt() {
	if m.goals == nil || !m.goals.Active() {
		return
	}
	_ = m.goals.UpdateStatus(ligoal.Paused)
	m.goalRequestTokens = 0
}

func (m *ChatModel) interruptGoalOnFailure() {
	if m == nil || !m.turnGoalManaged || m.goals == nil || !m.goals.Active() {
		return
	}
	_ = m.goals.UpdateStatus(ligoal.Interrupted)
	m.goalRequestTokens = 0
}

func (m *ChatModel) markRecoveredGoalInterrupted() bool {
	if m == nil || m.goals == nil || !m.goals.Active() {
		return false
	}
	_ = m.goals.UpdateStatus(ligoal.Interrupted)
	if m.plans != nil {
		_, _, _ = m.plans.SetMode(planstate.Build)
		m.syncAgentModePresentation()
	}
	m.goalRequestTokens = 0
	return true
}

func (m *ChatModel) goalCanResume() bool {
	state := m.goalStatePointer()
	if state == nil {
		return false
	}
	switch state.Status {
	case ligoal.Paused, ligoal.Blocked, ligoal.Interrupted, ligoal.Complete:
		return true
	default:
		return false
	}
}

func isPlainGoalResume(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "continue", "continuar", "resume", "reanudar":
		return true
	default:
		return false
	}
}

func (m *ChatModel) goalResumeView(w int) string {
	state := m.goalStatePointer()
	if state == nil || state.Status != ligoal.Interrupted {
		return ""
	}
	width := chatPaddedContentWidth(w)
	body := "GOAL INTERRUMPIDO · " + truncateOneLine(state.Objective, max(12, width-34)) + "  [ Continuar ]"
	return tuistyle.NewStyle().Width(width).Padding(0, 1).Foreground(m.ctx.Styles.Theme.Warning).Bold(true).Render(body)
}

func (m *ChatModel) handleGoalResumeMouse(v uikit.MouseMsg) (bool, uikit.Cmd) {
	if m == nil || m.ctx == nil || m.userScrolled || m.goalResumeView(m.ctx.Width) == "" || m.planQuestion.open || m.hasPendingPermission() {
		return false, nil
	}
	e, ok := mouseLeftPress(v)
	if !ok {
		return false, nil
	}
	used, maxCtx := m.contextUsage()
	y := m.viewportHeightForFrame(m.ctx.Width, m.ctx.Height, used, maxCtx)
	if e.Y != y {
		return false, nil
	}
	return true, m.runGoalCommand("resume")
}

func (m *ChatModel) goalPromptBlock() string {
	if m == nil || m.goals == nil {
		return ""
	}
	return m.goals.PromptBlock()
}
