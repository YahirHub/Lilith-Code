package goal

import "testing"

func TestGoalBudget(t *testing.T) {
	b := int64(10)
	m := NewManager(nil)
	if _, e := m.Set("ship", &b); e != nil {
		t.Fatal(e)
	}
	m.AddUsage(11)
	if m.Snapshot().Status != BudgetLimited {
		t.Fatal(m.Snapshot().Status)
	}
}
func TestGoalComplete(t *testing.T) {
	m := NewManager(nil)
	_, _ = m.Set("x", nil)
	if e := m.UpdateStatus(Complete); e != nil {
		t.Fatal(e)
	}
	if m.Active() {
		t.Fatal("complete goal active")
	}
}
