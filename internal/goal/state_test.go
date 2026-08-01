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
