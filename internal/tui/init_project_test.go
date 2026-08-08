package tui

import (
	"strings"
	"testing"
)

func TestInitAdditionalInstructionsAreOneShotAndDoNotCreateGoal(t *testing.T) {
	m := newInputTestChat(t)
	m.project = t.TempDir()
	cmd := m.runInit("actualiza la documentación de modules/")
	if cmd == nil || m.activeTurnID == 0 {
		t.Fatal("/init with instructions did not start")
	}
	if state := m.goals.Snapshot(); state != nil {
		t.Fatalf("/init created a durable goal: %+v", state)
	}
	if len(m.messages) == 0 || m.messages[0].Content != "/init actualiza la documentación de modules/" {
		t.Fatalf("visible command=%#v", m.messages)
	}
	if len(m.history) == 0 {
		t.Fatal("missing init provider prompt")
	}
	prompt := m.history[len(m.history)-1].Content
	if !strings.Contains(prompt, "<additional_init_instructions>\nactualiza la documentación de modules/\n</additional_init_instructions>") ||
		!strings.Contains(prompt, "do not create or replace a durable Goal") {
		t.Fatalf("one-shot block missing:\n%s", prompt)
	}
	m.endTurn()
}
