package tui

import (
	"testing"

	ligoal "github.com/lilith/li/internal/goal"
)

func TestCompletedGoalDoesNotStopLaterUserTurn(t *testing.T) {
	m := newInputTestChat(t)
	if _, err := m.goals.Set("crear el proyecto", nil); err != nil {
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
	if _, err := m.goals.Set("terminar la implementación", nil); err != nil {
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
