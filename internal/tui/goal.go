package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/lilith/li/internal/tui/uikit"

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
	return fmt.Sprintf("Goal · %s\n%s\nTokens usados: %d · Tiempo: %s · Sin límites artificiales", goalStatusLabel(s.Status), s.Objective, s.TokensUsed, (time.Duration(s.TimeUsedSeconds) * time.Second).String())
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
		case "resume", "reanudar":
			if err := mgr.UpdateStatus(ligoal.Active); err != nil {
				m.AddError(err.Error())
				return nil
			}
			m.AddSystem("Goal reanudado.")
			m.persistGoalState()
			if m.activeTurnID != 0 {
				m.turnGoalManaged = true
				m.enqueue("El goal persistente fue reanudado. Continúa trabajando hacia el objetivo activo.", queueSteer)
				return nil
			}
			return m.startGoalContinuation("Reanuda el goal persistente y continúa trabajando de forma autónoma hasta completarlo o quedar realmente bloqueado.")
		case "complete", "completed", "completar":
			if err := mgr.UpdateStatus(ligoal.Complete); err != nil {
				m.AddError(err.Error())
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
	mode := m.selectedAgentMode()
	m.activeTools = m.selectToolsForPrompt(m.goals.Snapshot().Objective+"\n"+instruction, mode)
	if err := m.beginTurnMode(mode); err != nil {
		m.AddError(err.Error())
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
	m.appendHistory(openai.Message{Role: "user", Content: "<goal_continue>Continue working autonomously toward the active durable goal. Do not stop merely to report progress. Finish the objective, or use update_goal with status=blocked/complete when appropriate.</goal_continue>"})
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

func (m *ChatModel) resumeActiveGoalCmd() uikit.Cmd {
	if m == nil || m.goals == nil || !m.goals.Active() || m.activeTurnID != 0 {
		return nil
	}
	return m.startGoalContinuation("La sesión se reanudó con un goal activo. Continúa trabajando autónomamente desde el estado persistido hasta completarlo o quedar realmente bloqueado.")
}

func (m *ChatModel) goalPromptBlock() string {
	if m == nil || m.goals == nil {
		return ""
	}
	return m.goals.PromptBlock()
}
