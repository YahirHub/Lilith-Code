package subagents

import "time"

// EventKind describes one observable lifecycle/progress transition of an
// isolated subagent. The runtime emits these events synchronously while the
// child works; hosts may stream them into a TUI, logs or telemetry without
// coupling the subagent package to the terminal UI runtime.
type EventKind string

const (
	EventStarted      EventKind = "started"
	EventThinking     EventKind = "thinking"
	EventText         EventKind = "text"
	EventStreamReset  EventKind = "stream_reset"
	EventToolStarted  EventKind = "tool_started"
	EventToolFinished EventKind = "tool_finished"
	EventCompleted    EventKind = "completed"
	EventFailed       EventKind = "failed"
	EventCanceled     EventKind = "canceled"
)

// Event is intentionally compact. The complete child transcript remains in
// the persisted subagent session; this stream carries just enough information
// to render Claude/OpenClaude-style live progress in the parent conversation.
type Event struct {
	Kind         EventKind
	TaskID       string
	ParentTaskID string
	AgentName    string
	Description  string
	Model        string
	Depth        int
	Resumed      bool
	Background   bool

	ToolCallID string
	ToolName   string
	ToolArgs   string
	Content    string

	At time.Time
}

// EventSink receives progress events. Callers should keep it cheap because it
// runs on the worker goroutine. A nil sink disables progress reporting.
type EventSink func(Event)
