package plan

import "testing"

func TestTogglePreservesReadyPlanAndSchedulesBuildHandoff(t *testing.T) {
	m := NewManager(nil)
	if m.Mode() != Build {
		t.Fatalf("default mode = %q", m.Mode())
	}
	m.Toggle()
	if m.Mode() != Plan {
		t.Fatalf("toggle should select plan, got %q", m.Mode())
	}
	if _, err := m.Complete("1. inspect\n2. change\n3. test"); err != nil {
		t.Fatal(err)
	}
	m.Toggle()
	if m.Mode() != Build {
		t.Fatalf("toggle should select build, got %q", m.Mode())
	}
	got, ok := m.ConsumeBuildHandoff()
	if !ok || got == "" {
		t.Fatalf("expected one-shot build handoff, got %q %v", got, ok)
	}
	if _, ok := m.ConsumeBuildHandoff(); ok {
		t.Fatal("handoff must be consumed once")
	}
}

func TestPlanQuestionsClearOnNextPlanUserTurn(t *testing.T) {
	m := NewManager(nil)
	_, _, _ = m.SetMode(Plan)
	if _, err := m.SetQuestions([]Question{{ID: "api", Question: "Qué API?", Options: []Option{{Label: "A"}, {Label: "B"}}}}); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().Questions) != 1 {
		t.Fatal("question not stored")
	}
	m.BeginUserTurn(Plan)
	if len(m.Snapshot().Questions) != 0 {
		t.Fatal("question should clear after user answer turn starts")
	}
}

func TestRunningPlanCanCompleteAfterUserSelectsBuild(t *testing.T) {
	m := NewManager(nil)
	_, _, _ = m.SetMode(Plan)
	// The turn starts in Plan, then the user presses Tab before it finishes.
	_, _, _ = m.SetMode(Build)
	if _, err := m.CompleteFor(Plan, "inspect\nchange\ntest"); err != nil {
		t.Fatal(err)
	}
	if m.Mode() != Build {
		t.Fatalf("selected next mode changed unexpectedly: %q", m.Mode())
	}
	got, ok := m.ConsumeBuildHandoff()
	if !ok || got == "" {
		t.Fatalf("completed running Plan turn must hand off to already-selected Build: %q %v", got, ok)
	}
}
