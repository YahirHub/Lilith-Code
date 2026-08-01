package tui

import (
	"testing"

	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
)

func TestCompletedGoalDoesNotStopLaterUserTurn(t *testing.T) {
	m := newInputTestChat(t)
	if _, err := m.goals.Set("crear el proyecto"); err != nil {
		t.Fatal(err)
	}
	if err := m.goals.UpdateStatus(ligoal.Complete); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.submit("corrige el manejo del código QR")
	if cmd == nil || m.activeTurnID == 0 {
		t.Fatal("una solicitud normal posterior debe iniciar un turno")
	}
	if m.turnGoalManaged {
		t.Fatal("un goal ya completado no debe asociarse al nuevo turno del usuario")
	}
	if m.goalStopsCurrentLoop() {
		t.Fatal("el goal completado no debe cortar el turno normal en su primera tool")
	}
	m.endTurn()
}

func TestGoalManagedTurnStopsOnlyAfterItsGoalChangesState(t *testing.T) {
	m := newInputTestChat(t)
	if _, err := m.goals.Set("terminar la implementación"); err != nil {
		t.Fatal(err)
	}
	if err := m.beginTurn(); err != nil {
		t.Fatal(err)
	}
	if !m.turnGoalManaged || m.goalStopsCurrentLoop() {
		t.Fatalf("el turno debe estar ligado al goal activo: managed=%v stop=%v", m.turnGoalManaged, m.goalStopsCurrentLoop())
	}

	if err := m.goals.UpdateStatus(ligoal.Complete); err != nil {
		t.Fatal(err)
	}
	if !m.goalStopsCurrentLoop() {
		t.Fatal("el mismo turno autónomo sí debe detenerse cuando su goal se completa")
	}
	if err := m.goals.UpdateStatus(ligoal.Active); err != nil {
		t.Fatal(err)
	}
	m.goals.Clear()
	if !m.goalStopsCurrentLoop() {
		t.Fatal("eliminar el goal durante su propio loop también debe detenerlo")
	}
	m.endTurn()
	if m.turnGoalManaged {
		t.Fatal("endTurn debe limpiar la asociación con el goal")
	}
}

func TestGoalCreatedDuringRunningTurnBindsThatTurn(t *testing.T) {
	m := newInputTestChat(t)
	if err := m.beginTurn(); err != nil {
		t.Fatal(err)
	}
	if m.turnGoalManaged {
		t.Fatal("el turno comenzó sin goal")
	}

	_ = m.runGoalCommand("terminar la corrección")
	if !m.turnGoalManaged {
		t.Fatal("crear un goal durante un turno debe convertir ese turno en su ejecución administrada")
	}
	m.endTurn()
}

func TestTabCyclesBuildPlanGoalAndShiftTabReverses(t *testing.T) {
	m := newInputTestChat(t)
	if got := m.selectedAgentMode(); got != planstate.Build {
		t.Fatalf("modo inicial = %q", got)
	}
	m.cycleAgentMode(1)
	if got := m.selectedAgentMode(); got != planstate.Plan || m.textarea.Prompt != "plan ❯ " {
		t.Fatalf("primer Tab = %q prompt=%q", got, m.textarea.Prompt)
	}
	m.cycleAgentMode(1)
	if got := m.selectedAgentMode(); got != planstate.Goal || m.textarea.Prompt != "goal ❯ " {
		t.Fatalf("segundo Tab = %q prompt=%q", got, m.textarea.Prompt)
	}
	m.cycleAgentMode(1)
	if got := m.selectedAgentMode(); got != planstate.Build || m.textarea.Prompt != "build ❯ " {
		t.Fatalf("tercer Tab = %q prompt=%q", got, m.textarea.Prompt)
	}
	m.cycleAgentMode(-1)
	if got := m.selectedAgentMode(); got != planstate.Goal {
		t.Fatalf("Shift+Tab = %q", got)
	}
}

func TestGoalModeTurnsPlainInputIntoDurableGoal(t *testing.T) {
	m := newInputTestChat(t)
	m.setAgentMode(planstate.Goal)
	_, cmd := m.submit("terminar la migración y verificarla")
	if cmd == nil {
		t.Fatal("goal mode debe iniciar la continuación autónoma")
	}
	state := m.goals.Snapshot()
	if state == nil || state.Objective != "terminar la migración y verificarla" {
		t.Fatalf("goal = %#v", state)
	}
	if m.activeTurnID == 0 || m.turnAgentMode != planstate.Goal {
		t.Fatalf("turno goal no iniciado: id=%d mode=%q", m.activeTurnID, m.turnAgentMode)
	}
	if len(m.messages) == 0 || m.messages[0].Kind != MsgUser {
		t.Fatalf("la instrucción goal debe permanecer visible en el transcript: %#v", m.messages)
	}
	m.endTurn()
}

func TestDeprecatedGoalBudgetIsIgnored(t *testing.T) {
	objective, deprecated, err := parseGoalArgs("--tokens 10 terminar la migración")
	if err != nil {
		t.Fatal(err)
	}
	if !deprecated || objective != "terminar la migración" {
		t.Fatalf("objective=%q deprecated=%v", objective, deprecated)
	}
}
