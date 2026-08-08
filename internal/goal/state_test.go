package goal

import "testing"

func TestGoalUsageNeverStopsExecution(t *testing.T) {
	m := NewManager(nil)
	if _, err := m.Set("ship"); err != nil {
		t.Fatal(err)
	}
	m.AddUsage(1_000_000_000)
	s := m.Snapshot()
	if s.Status != Active {
		t.Fatalf("usage must remain diagnostic; status=%q", s.Status)
	}
	if s.TokensUsed != 1_000_000_000 {
		t.Fatalf("tokens used=%d", s.TokensUsed)
	}
}

func TestLegacyLimitStatusesResumeAsActive(t *testing.T) {
	for _, legacy := range []Status{"budget_limited", "usage_limited"} {
		m := NewManager(&State{Objective: "continue", Status: legacy})
		if got := m.Snapshot().Status; got != Active {
			t.Fatalf("legacy status %q migrated to %q", legacy, got)
		}
	}
}

func TestGoalComplete(t *testing.T) {
	m := NewManager(nil)
	_, _ = m.Set("x")
	if err := m.UpdateStatus(Complete); err != nil {
		t.Fatal(err)
	}
	if m.Active() {
		t.Fatal("complete goal active")
	}
}

func TestSettingSameActiveGoalIsIdempotent(t *testing.T) {
	m := NewManager(nil)
	first, err := m.Set("ship the project")
	if err != nil {
		t.Fatal(err)
	}
	m.AddUsage(1234)
	before := m.Snapshot()
	second, err := m.Set("ship the project")
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedAt != first.CreatedAt || second.TokensUsed != before.TokensUsed || second.Status != Active {
		t.Fatalf("duplicate active goal reset state: first=%+v before=%+v second=%+v", first, before, second)
	}
}

func TestCompleteStoresSummaryAndResumePreservesObjectiveAndUsage(t *testing.T) {
	m := NewManager(nil)
	first, err := m.Set("publicar la versión")
	if err != nil {
		t.Fatal(err)
	}
	m.AddUsage(321)
	if err := m.Complete("Versión publicada y pruebas verificadas."); err != nil {
		t.Fatal(err)
	}
	completed := m.Snapshot()
	if completed.Status != Complete || completed.Summary == "" {
		t.Fatalf("completed=%+v", completed)
	}
	if err := m.Resume(); err != nil {
		t.Fatal(err)
	}
	resumed := m.Snapshot()
	if resumed.Status != Active || resumed.Objective != first.Objective || resumed.CreatedAt != first.CreatedAt || resumed.TokensUsed != 321 || resumed.Summary != "" {
		t.Fatalf("resumed=%+v first=%+v", resumed, first)
	}
}

func TestInterruptedStatusSurvivesLoad(t *testing.T) {
	m := NewManager(&State{Objective: "continuar", Status: Interrupted})
	if got := m.Snapshot().Status; got != Interrupted {
		t.Fatalf("status=%q", got)
	}
}
