package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/subagents"
)

func TestRecoverPendingBackgroundAgentCompletionAfterRestart(t *testing.T) {
	finished := time.Date(2026, 8, 3, 20, 0, 0, 123, time.UTC)
	panel := &AgentPanel{
		TaskID: "agent-1", Name: "reviewer", Background: true, Status: "completed",
		Output: "review complete", FinishedAt: finished,
	}
	m := &ChatModel{messages: []ChatMessage{{Kind: MsgAgent, Agent: panel}}}

	m.recoverPendingBackgroundAgentMessages()
	m.recoverPendingBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 1 {
		t.Fatalf("pending=%#v", m.pendingBackgroundAgentMessages)
	}
	note := m.pendingBackgroundAgentMessages[0].Content
	if !strings.Contains(note, `task_id="agent-1"`) || !strings.Contains(note, `finished_at="2026-08-03T20:00:00.000000123Z"`) || !strings.Contains(note, "review complete") {
		t.Fatalf("note=%q", note)
	}

	m.deliverBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 0 || len(m.history) != 1 {
		t.Fatalf("pending=%d history=%#v", len(m.pendingBackgroundAgentMessages), m.history)
	}
	m.recoverPendingBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 0 {
		t.Fatal("delivered completion was queued again")
	}
}

func TestBackgroundCompletionAllowsLaterResumeOfSameTask(t *testing.T) {
	first := time.Date(2026, 8, 3, 20, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	history := []openai.Message{backgroundAgentCompletionMessage("agent-1", "worker", "completed", "first", first)}
	m := &ChatModel{
		history: history,
		messages: []ChatMessage{{Kind: MsgAgent, Agent: &AgentPanel{
			TaskID: "agent-1", Name: "worker", Background: true, Status: "completed", Output: "second", FinishedAt: second,
		}}},
	}
	m.recoverPendingBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 1 || !strings.Contains(m.pendingBackgroundAgentMessages[0].Content, second.Format(time.RFC3339Nano)) {
		t.Fatalf("resumed completion not queued: %#v", m.pendingBackgroundAgentMessages)
	}
}

func TestApplyBackgroundTerminalEventCreatesPanelAndQueuesOnce(t *testing.T) {
	finished := time.Date(2026, 8, 3, 21, 0, 0, 0, time.UTC)
	m := &ChatModel{}
	event := subagents.Event{
		Kind: subagents.EventFailed, TaskID: "agent-failed", AgentName: "audit", Background: true,
		Content: "provider unavailable", At: finished,
	}
	m.applyAgentEvent(event)
	m.applyAgentEvent(event)
	if panel := m.agentPanels[event.TaskID]; panel == nil || panel.Status != "failed" {
		t.Fatalf("panel=%#v", panel)
	}
	if len(m.pendingBackgroundAgentMessages) != 1 {
		t.Fatalf("duplicate terminal notification: %#v", m.pendingBackgroundAgentMessages)
	}
}

func TestAgentEventSinkDropsEventsFromPreviousSession(t *testing.T) {
	m := &ChatModel{agentEventCh: make(chan agentEventEnvelope, 2)}
	m.sessionCtx, m.sessionCancel = context.WithCancel(context.Background())
	m.agentGeneration.Store(1)
	oldSink := m.agentEventSink()

	// Queue one event before the boundary. Generation checks in the sink alone
	// cannot protect against an event that is already buffered or batched.
	oldSink(subagents.Event{Kind: subagents.EventCompleted, TaskID: "queued-old"})
	m.resetAgentSessionContext()
	queued := <-m.agentEventCh
	if m.applyAgentEventEnvelope(queued) {
		t.Fatal("queued stale event crossed session boundary")
	}

	// Events produced after cancellation are rejected before they enter the
	// channel as well.
	oldSink(subagents.Event{Kind: subagents.EventCompleted, TaskID: "late-old"})
	select {
	case envelope := <-m.agentEventCh:
		t.Fatalf("late stale event crossed session boundary: %#v", envelope)
	default:
	}

	newSink := m.agentEventSink()
	newSink(subagents.Event{Kind: subagents.EventCompleted, TaskID: "new"})
	select {
	case envelope := <-m.agentEventCh:
		if !m.applyAgentEventEnvelope(envelope) || envelope.event.TaskID != "new" {
			t.Fatalf("envelope=%#v", envelope)
		}
	case <-time.After(time.Second):
		t.Fatal("current session event was dropped")
	}
}

func TestDeliverBackgroundCompletionKeepsCurrentUserPromptLast(t *testing.T) {
	finished := time.Date(2026, 8, 3, 22, 0, 0, 0, time.UTC)
	m := &ChatModel{
		history: []openai.Message{{Role: "user", Content: "corrige el orquestador"}},
		pendingBackgroundAgentMessages: []openai.Message{
			backgroundAgentCompletionMessage("agent-1", "reviewer", "completed", "hallazgo", finished),
		},
	}
	m.deliverBackgroundAgentMessages()
	if len(m.history) != 2 {
		t.Fatalf("history=%#v", m.history)
	}
	if !strings.Contains(m.history[0].Content, "<background_agent_completion ") || m.history[1].Content != "corrige el orquestador" {
		t.Fatalf("completion displaced current prompt: %#v", m.history)
	}
}

func TestDeliverBackgroundCompletionAppendsAfterToolBoundary(t *testing.T) {
	finished := time.Date(2026, 8, 3, 22, 0, 0, 0, time.UTC)
	m := &ChatModel{
		history: []openai.Message{{Role: "tool", ToolCallID: "call-1", Content: "done"}},
		pendingBackgroundAgentMessages: []openai.Message{
			backgroundAgentCompletionMessage("agent-1", "reviewer", "completed", "hallazgo", finished),
		},
	}
	m.deliverBackgroundAgentMessages()
	if len(m.history) != 2 || m.history[0].Role != "tool" || !strings.Contains(m.history[1].Content, "<background_agent_completion ") {
		t.Fatalf("history=%#v", m.history)
	}
}

func TestAgentPanelTerminalEventsAreIdempotent(t *testing.T) {
	finished := time.Date(2026, 8, 3, 22, 30, 0, 0, time.UTC)
	panel := &AgentPanel{TaskID: "agent-1"}
	event := subagents.Event{
		Kind: subagents.EventFailed, TaskID: "agent-1", AgentName: "audit",
		Content: "provider unavailable", At: finished,
	}
	panel.Apply(event)
	panel.Apply(event)
	if strings.Count(panel.Output, "Error: provider unavailable") != 1 {
		t.Fatalf("terminal event duplicated output: %q", panel.Output)
	}
	if panel.Status != "failed" || !panel.FinishedAt.Equal(finished) {
		t.Fatalf("panel=%#v", panel)
	}
}

func TestRecoverMigratesLegacyCompletionWithoutSilencingLaterResume(t *testing.T) {
	first := time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	legacy := openai.Message{Role: "user", Content: `<background_agent_completion task_id="agent-legacy" agent="worker" status="completed">\nfirst\n</background_agent_completion>`}
	panel := &AgentPanel{
		TaskID: "agent-legacy", Name: "worker", Background: true,
		Status: "completed", Output: "first", FinishedAt: first,
	}
	m := &ChatModel{
		history:  []openai.Message{legacy},
		messages: []ChatMessage{{Kind: MsgAgent, Agent: panel}},
	}

	m.recoverPendingBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 0 {
		t.Fatalf("legacy completion was duplicated: %#v", m.pendingBackgroundAgentMessages)
	}
	if !strings.Contains(m.history[0].Content, `finished_at="`+first.Format(time.RFC3339Nano)+`"`) {
		t.Fatalf("legacy marker was not migrated: %q", m.history[0].Content)
	}

	panel.Output = "second"
	panel.FinishedAt = second
	m.recoverPendingBackgroundAgentMessages()
	if len(m.pendingBackgroundAgentMessages) != 1 || !strings.Contains(m.pendingBackgroundAgentMessages[0].Content, second.Format(time.RFC3339Nano)) {
		t.Fatalf("later resume was silenced: %#v", m.pendingBackgroundAgentMessages)
	}
}

func TestFinalizeRunningAgentPanelsClosesPreviousSessionExactlyOnce(t *testing.T) {
	background := &AgentPanel{TaskID: "agent-bg", Name: "worker", Status: "running", Background: true}
	foreground := &AgentPanel{TaskID: "agent-fg", Name: "reviewer", Status: "running"}
	completed := &AgentPanel{TaskID: "agent-done", Name: "done", Status: "completed", FinishedAt: time.Now()}
	m := &ChatModel{agentPanels: map[string]*AgentPanel{
		background.TaskID: background,
		foreground.TaskID: foreground,
		completed.TaskID:  completed,
	}}

	if !m.finalizeRunningAgentPanels("Cancelado al cambiar de sesión.") {
		t.Fatal("running panels were not finalized")
	}
	for _, panel := range []*AgentPanel{background, foreground} {
		if panel.Status != "killed" || panel.FinishedAt.IsZero() || strings.Count(panel.Output, "Cancelado al cambiar de sesión.") != 1 {
			t.Fatalf("panel=%#v", panel)
		}
	}
	if completed.Status != "completed" {
		t.Fatalf("completed panel was modified: %#v", completed)
	}
	if m.finalizeRunningAgentPanels("Cancelado al cambiar de sesión.") {
		t.Fatal("terminal panels were finalized twice")
	}
	for _, panel := range []*AgentPanel{background, foreground} {
		if strings.Count(panel.Output, "Cancelado al cambiar de sesión.") != 1 {
			t.Fatalf("reason duplicated: %#v", panel)
		}
	}
}
