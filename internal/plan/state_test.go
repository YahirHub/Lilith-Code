package plan

import (
	"strings"
	"testing"
)

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

func TestPlanQuestionsSurviveModeToggleUntilRealUserTurn(t *testing.T) {
	m := NewManager(nil)
	_, _, _ = m.SetMode(Plan)
	if _, err := m.SetQuestions([]Question{{ID: "ui", Question: "Qué UI?", Options: []Option{{Label: "Fyne"}, {Label: "Wails"}}}}); err != nil {
		t.Fatal(err)
	}
	_, _, _ = m.SetMode(Build)
	_, _, _ = m.SetMode(Plan)
	if len(m.Snapshot().Questions) != 1 {
		t.Fatal("mode cycling must not dismiss the pending human decision")
	}
	m.BeginUserTurn(Build)
	if len(m.Snapshot().Questions) != 0 {
		t.Fatal("an actual new user turn must supersede stale pending questions")
	}
}

func TestPlanQuestionAnswersPersistOneByOne(t *testing.T) {
	m := NewManager(nil)
	_, _, _ = m.SetMode(Plan)
	questions := []Question{
		{ID: "ui", Question: "Qué UI?", Options: []Option{{Label: "Fyne"}, {Label: "Wails"}}},
		{ID: "shape", Question: "Cómo se integra?", Options: []Option{{Label: "Subcomando"}, {Label: "Binario"}}},
	}
	if _, err := m.SetQuestions(questions); err != nil {
		t.Fatal(err)
	}
	state, complete, err := m.AnswerQuestion("ui", "Fyne")
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("first of two answers must not complete the request")
	}
	if got := state.QuestionAnswers["ui"]; got != "Fyne" {
		t.Fatalf("stored answer = %q", got)
	}
	if idx := PendingQuestionIndex(state); idx != 1 {
		t.Fatalf("next unanswered question index = %d, want 1", idx)
	}

	// Simulate session serialization/restore through a cloned snapshot.
	restored := NewManager(&state)
	if got := restored.Snapshot().QuestionAnswers["ui"]; got != "Fyne" {
		t.Fatalf("restored partial answer = %q", got)
	}
	state, complete, err = restored.AnswerQuestion("shape", "Subcomando")
	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Fatal("second answer should complete the request")
	}
	if PendingQuestionIndex(state) != -1 {
		t.Fatal("all questions should be answered")
	}
	text := FormatQuestionAnswers(state)
	if text == "" || !containsAll(text, "Qué UI?", "Fyne", "Cómo se integra?", "Subcomando") {
		t.Fatalf("unexpected formatted answers: %q", text)
	}
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}
