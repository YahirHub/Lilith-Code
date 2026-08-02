package tui

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers/openai"
)

func TestCompactHistoryForRequestKeepsRecentTurnsAndDurableHistory(t *testing.T) {
	largeOutput := strings.Repeat("tool-output-", 400)
	largeBody := strings.Repeat("file-body-", 400)
	call := openai.ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "create_file"
	call.Function.Arguments = `{"path":"old.txt","content":"` + largeBody + `"}`

	history := []openai.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", ToolCalls: []openai.ToolCall{call}},
		{Role: "tool", Name: "create_file", ToolCallID: "call-1", Content: largeOutput},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "turn three"},
	}

	got := compactHistoryForRequest(history)
	if len(got[2].Content) >= len(largeOutput) || !strings.Contains(got[2].Content, "compacted") {
		t.Fatalf("old tool output was not compacted: %d >= %d", len(got[2].Content), len(largeOutput))
	}
	if len(got[1].ToolCalls[0].Function.Arguments) >= len(call.Function.Arguments) {
		t.Fatalf("old tool input was not compacted")
	}
	if got[4].Content != history[4].Content || got[6].Content != history[6].Content {
		t.Fatalf("recent user turns must stay verbatim")
	}
	if history[2].Content != largeOutput || history[1].ToolCalls[0].Function.Arguments != call.Function.Arguments {
		t.Fatalf("request compaction mutated durable history")
	}
}

func TestAppendRuntimeStateOnlyTouchesLatestUserCopy(t *testing.T) {
	history := []openai.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "current"},
		{Role: "tool", Content: "tool"},
	}
	copyHistory := cloneHistoryMessages(history)
	got := appendRuntimeState(copyHistory, "<todo_state>active</todo_state>")
	if got[0].Content != "first" {
		t.Fatalf("older prompt prefix changed: %q", got[0].Content)
	}
	if !strings.Contains(got[2].Content, "<lilith_runtime_state>") {
		t.Fatalf("runtime state not appended to latest user message: %q", got[2].Content)
	}
	if history[2].Content != "current" {
		t.Fatalf("original history mutated")
	}
}

func TestAppendCodeIntelSystemProfileStaysOutOfUserTurn(t *testing.T) {
	system := appendCodeIntelSystemProfile("base system", "profile")
	if !strings.Contains(system, "<code_intelligence>") || !strings.Contains(system, "profile") {
		t.Fatalf("code-intelligence profile missing from system content: %q", system)
	}
	if strings.Count(appendCodeIntelSystemProfile(system, "profile"), "<code_intelligence>") != 1 {
		t.Fatalf("code-intelligence profile was duplicated: %q", system)
	}

	history := []openai.Message{{Role: "user", Content: "task"}}
	got := appendRuntimeState(cloneHistoryMessages(history), "<todo_state>active</todo_state>")
	if strings.Contains(got[0].Content, "<code_intelligence>") {
		t.Fatalf("code-intelligence profile leaked into the user turn: %q", got[0].Content)
	}
}
