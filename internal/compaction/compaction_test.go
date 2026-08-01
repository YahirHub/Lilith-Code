package compaction

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lilith/li/internal/providers/openai"
)

func TestPrepareKeepsRecentCompleteTurns(t *testing.T) {
	history := []openai.Message{
		{Role: "user", Content: strings.Repeat("old-1 ", 200)},
		{Role: "assistant", Content: "done one"},
		{Role: "user", Content: strings.Repeat("old-2 ", 200)},
		{Role: "assistant", Content: "done two"},
		{Role: "user", Content: "recent one"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "recent two"},
	}
	plan, ok := Prepare(history, 30, 2)
	if !ok {
		t.Fatal("expected compaction plan")
	}
	if plan.CutIndex != 4 || len(plan.Head) != 4 || len(plan.Tail) != 3 {
		t.Fatalf("unexpected split: cut=%d head=%d tail=%d", plan.CutIndex, len(plan.Head), len(plan.Tail))
	}
	if plan.Tail[0].Role != "user" || plan.Tail[0].Content != "recent one" {
		t.Fatalf("tail must begin at complete user turn: %+v", plan.Tail[0])
	}
}

func TestPrepareCarriesPreviousSummaryIteratively(t *testing.T) {
	history := []openai.Message{
		SummaryMessage("old summary"),
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "answer one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "answer two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "answer three"},
		{Role: "user", Content: "turn four"},
	}
	plan, ok := Prepare(history, 10, 2)
	if !ok || plan.PreviousSummary != "old summary" {
		t.Fatalf("previous summary not carried: ok=%v summary=%q", ok, plan.PreviousSummary)
	}
	for _, msg := range plan.Head {
		if _, isSummary := SummaryFromMessage(msg); isSummary {
			t.Fatal("previous summary must not be serialized as ordinary history")
		}
	}
}

func TestApplyCreatesSummaryAndExactTail(t *testing.T) {
	plan := Plan{Tail: []openai.Message{{Role: "user", Content: "keep"}, {Role: "assistant", Content: "exact"}}}
	got := Apply(plan, "summary")
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	if summary, ok := SummaryFromMessage(got[0]); !ok || summary != "summary" {
		t.Fatalf("missing summary marker: %+v", got[0])
	}
	if got[1].Content != "keep" || got[2].Content != "exact" {
		t.Fatalf("tail changed: %+v", got[1:])
	}
}

func TestRecentTokenBudgetScalesForSmallModels(t *testing.T) {
	cases := map[int]int{
		0:       DefaultKeepRecentTokens,
		8_000:   2_000,
		16_000:  4_000,
		32_000:  8_000,
		128_000: DefaultKeepRecentTokens,
	}
	for window, expected := range cases {
		if got := RecentTokenBudget(window); got != expected {
			t.Fatalf("window %d: got %d want %d", window, got, expected)
		}
	}
}

func TestNeedsAutoUsesReserve(t *testing.T) {
	msgs := []openai.Message{{Role: "user", Content: strings.Repeat("x", 3600)}} // ~904 tokens
	if !NeedsAuto(msgs, 1000, 100) {
		t.Fatal("expected proactive compaction at threshold")
	}
	if NeedsAuto(msgs, 2000, 100) {
		t.Fatal("must remain below a larger threshold")
	}
}

func TestSerializeBoundsToolPayloads(t *testing.T) {
	call := openai.ToolCall{ID: "call", Type: "function"}
	call.Function.Name = "create_file"
	call.Function.Arguments = strings.Repeat("a", 10_000)
	text := Serialize([]openai.Message{{Role: "assistant", ToolCalls: []openai.ToolCall{call}}, {Role: "tool", Content: strings.Repeat("b", 20_000)}})
	if len(text) >= 20_000 || !strings.Contains(text, "compacted") {
		t.Fatalf("serialized payload was not bounded: %d", len(text))
	}
}

func TestPrepareSplitsOneHugeTurnAtAssistantBoundary(t *testing.T) {
	first := openai.ToolCall{ID: "first", Type: "function"}
	first.Function.Name = "read_file"
	first.Function.Arguments = `{"path":"large.txt"}`
	second := openai.ToolCall{ID: "second", Type: "function"}
	second.Function.Name = "run_terminal_command"
	second.Function.Arguments = `{"command":"go test ./..."}`
	history := []openai.Message{
		{Role: "user", Content: "complete the project"},
		{Role: "assistant", Content: strings.Repeat("old analysis ", 300), ToolCalls: []openai.ToolCall{first}},
		{Role: "tool", ToolCallID: "first", Name: "read_file", Content: strings.Repeat("old output ", 300)},
		{Role: "assistant", ToolCalls: []openai.ToolCall{second}},
		{Role: "tool", ToolCallID: "second", Name: "run_terminal_command", Content: "tests still running"},
	}
	plan, ok := Prepare(history, 40, 2)
	if !ok {
		t.Fatal("expected split-turn compaction")
	}
	if !plan.SplitTurn || plan.FullCompaction {
		t.Fatalf("unexpected split flags: %+v", plan)
	}
	if plan.CutIndex != 3 || len(plan.Tail) != 2 || plan.Tail[0].Role != "assistant" {
		t.Fatalf("split must retain a protocol-safe assistant/tool suffix: cut=%d tail=%+v", plan.CutIndex, plan.Tail)
	}
	if plan.Tail[1].Role != "tool" || plan.Tail[1].ToolCallID != "second" {
		t.Fatalf("tool result separated from its call: %+v", plan.Tail)
	}
}

func TestPrepareFallsBackToFullSummaryWhenNoSafeSuffixFits(t *testing.T) {
	call := openai.ToolCall{ID: "huge", Type: "function"}
	call.Function.Name = "read_file"
	history := []openai.Message{
		{Role: "user", Content: "inspect huge output"},
		{Role: "assistant", ToolCalls: []openai.ToolCall{call}},
		{Role: "tool", ToolCallID: "huge", Name: "read_file", Content: strings.Repeat("x", 20_000)},
	}
	plan, ok := Prepare(history, 100, 2)
	if !ok || !plan.FullCompaction || len(plan.Tail) != 0 || plan.CutIndex != len(history) {
		t.Fatalf("expected full-summary fallback, got ok=%v plan=%+v", ok, plan)
	}
}

func TestBuildSummaryMessagesIncludesCustomAndSplitGuidance(t *testing.T) {
	plan := Plan{Head: []openai.Message{{Role: "user", Content: "do work"}}, SplitTurn: true, PreviousSummary: "older state"}
	msgs := BuildSummaryMessages(plan, "focus on failing tests")
	if len(msgs) != 2 {
		t.Fatalf("unexpected summary request: %+v", msgs)
	}
	body := msgs[1].Content
	for _, expected := range []string{"<previous_summary>", "inside one long agent turn", "focus on failing tests"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("summary request missing %q: %s", expected, body)
		}
	}
}

func TestSerializeTruncationPreservesUTF8(t *testing.T) {
	text := Serialize([]openai.Message{{Role: "tool", Content: strings.Repeat("á界", 3_000)}})
	if !strings.Contains(text, "compacted") || !utf8.ValidString(text) {
		t.Fatalf("bounded serialization must remain valid UTF-8")
	}
}

func TestAutoThresholdPreservesReserveWhenItFits(t *testing.T) {
	if got := AutoThreshold(32_000, DefaultReserveTokens); got != 15_616 {
		t.Fatalf("32k threshold=%d want 15616", got)
	}
	if got := AutoThreshold(8_000, DefaultReserveTokens); got <= 0 || got >= 8_000 {
		t.Fatalf("small-window fallback must stay usable, got %d", got)
	}
}

func TestNeedsAutoRequestIncludesToolSchemas(t *testing.T) {
	messages := []openai.Message{{Role: "user", Content: "small"}}
	schemas := []any{map[string]any{"type": "function", "description": strings.Repeat("x", 4_000)}}
	if NeedsAuto(messages, 1_000, 100) {
		t.Fatal("messages alone should remain below threshold")
	}
	if !NeedsAutoRequest(messages, schemas, 1_000, 100) {
		t.Fatal("tool schemas must count toward automatic compaction")
	}
}

func TestBuildSummaryMessagesBoundsPathologicalWholeConversation(t *testing.T) {
	head := make([]openai.Message, 0, 200)
	for i := 0; i < 200; i++ {
		head = append(head, openai.Message{Role: "assistant", Content: strings.Repeat("detail ", 2_000)})
	}
	messages := BuildSummaryMessages(Plan{Head: head}, "", 8_000)
	if len(messages) != 2 {
		t.Fatalf("unexpected summary messages: %d", len(messages))
	}
	if len(messages[1].Content) > 8_000*4 {
		t.Fatalf("summary conversation was not globally bounded: %d chars", len(messages[1].Content))
	}
	if !strings.Contains(messages[1].Content, "historical conversation compacted") {
		t.Fatal("expected a visible whole-conversation truncation marker")
	}
}

func TestPrepareOlderTurnsPreservesNewestCompleteTurn(t *testing.T) {
	history := []openai.Message{
		SummaryMessage("previous"),
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "current"},
	}
	plan, ok := PrepareOlderTurns(history)
	if !ok {
		t.Fatal("expected forced older-turn plan")
	}
	if plan.CutIndex != 3 || len(plan.Head) != 2 || len(plan.Tail) != 1 || plan.Tail[0].Content != "current" {
		t.Fatalf("unexpected forced split: %+v", plan)
	}
	if plan.PreviousSummary != "previous" {
		t.Fatalf("previous summary lost: %q", plan.PreviousSummary)
	}
}
