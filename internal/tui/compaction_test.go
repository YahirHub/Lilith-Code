package tui

import (
	"errors"
	"strings"
	"testing"

	compactctx "github.com/lilith/li/internal/compaction"
	"github.com/lilith/li/internal/providers/openai"
)

func TestContextOverflowDetection(t *testing.T) {
	for _, message := range []string{
		"context_length_exceeded",
		"maximum context length is 128000 tokens",
		"prompt is too long for this model",
		"input exceeds the model's context window",
	} {
		if !isContextOverflowError(errors.New(message)) {
			t.Fatalf("expected overflow for %q", message)
		}
	}
	if isContextOverflowError(errors.New("provider returned 401 unauthorized")) {
		t.Fatal("unrelated provider errors must not trigger compaction")
	}
}

func TestCompactCommandIsRegistered(t *testing.T) {
	for _, command := range Commands() {
		if command.Name != "compact" {
			continue
		}
		if command.Usage != "[instrucciones opcionales]" {
			t.Fatalf("unexpected /compact usage: %q", command.Usage)
		}
		if command.Run == nil {
			t.Fatal("/compact has no handler")
		}
		return
	}
	t.Fatal("/compact is not registered")
}

func TestApplyCompactionKeepsTranscriptAndArchivesProtocolHistory(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.history = []openai.Message{
		{Role: "user", Content: strings.Repeat("old request ", 100)},
		{Role: "assistant", Content: "old result"},
		{Role: "user", Content: "recent request one"},
		{Role: "assistant", Content: "recent answer one"},
		{Role: "user", Content: "recent request two"},
	}
	model.messages = []ChatMessage{
		{Kind: MsgUser, Content: "old request"},
		{Kind: MsgAssistant, Content: "old result"},
		{Kind: MsgUser, Content: "recent request one"},
		{Kind: MsgAssistant, Content: "recent answer one"},
		{Kind: MsgUser, Content: "recent request two"},
	}
	plan, ok := compactctx.Prepare(model.history, 1, 2)
	if !ok {
		t.Fatal("expected compaction plan")
	}
	model.compacting = true
	model.activeCompactionID = 7
	beforeTranscript := len(model.messages)
	late := openai.Message{Role: "user", Content: "mensaje agregado durante la compactación"}
	model.history = append(model.history, late)

	model.applyCompactionResult(compactionResultMsg{
		id: 7, plan: plan, summary: "Old work completed and verified.", instructions: "focus on tests",
	})

	if len(model.history) != len(plan.Tail)+2 {
		t.Fatalf("unexpected active history length: %d", len(model.history))
	}
	if model.history[len(model.history)-1].Content != late.Content {
		t.Fatalf("message appended during compaction was lost: %+v", model.history)
	}
	if summary, ok := compactctx.SummaryFromMessage(model.history[0]); !ok || !strings.Contains(summary, "Old work") {
		t.Fatalf("active history does not begin with summary: %+v", model.history[0])
	}
	if len(model.messages) != beforeTranscript+1 {
		t.Fatalf("visible transcript was replaced instead of preserved: before=%d after=%d", beforeTranscript, len(model.messages))
	}
	if model.sess == nil || len(model.sess.Compactions) != 1 {
		t.Fatalf("compaction archive missing: %+v", model.sess)
	}
	if model.autoCompactionSkipHistoryLen != 0 {
		t.Fatalf("successful compaction must allow one more preflight check, got guard=%d", model.autoCompactionSkipHistoryLen)
	}
	record := model.sess.Compactions[0]
	if len(record.ArchivedMessages) != plan.CutIndex || record.Instructions != "focus on tests" || record.SplitTurn != plan.SplitTurn || record.FullCompaction != plan.FullCompaction {
		t.Fatalf("unexpected archive record: %+v", record)
	}
}

func TestCompactionPlanUsesWirePruningButKeepsExactArchivedMessages(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	call := openai.ToolCall{ID: "old", Type: "function"}
	call.Function.Name = "read_files"
	call.Function.Arguments = `{"paths":["large.log"]}`
	largeOutput := strings.Repeat("raw-tool-output ", 5_000)
	model.history = []openai.Message{
		{Role: "user", Content: "old request"},
		{Role: "assistant", ToolCalls: []openai.ToolCall{call}},
		{Role: "tool", ToolCallID: "old", Name: "read_files", Content: largeOutput},
		{Role: "assistant", Content: strings.Repeat("old assistant detail ", 6_000)},
		{Role: "user", Content: "recent one"},
		{Role: "assistant", Content: "answer one"},
		{Role: "user", Content: "recent two"},
	}
	plan, ok := model.compactionPlan(false)
	if !ok {
		t.Fatal("expected compaction plan")
	}
	foundExact := false
	for _, message := range plan.Head {
		if message.Role == "tool" && message.Content == largeOutput {
			foundExact = true
		}
	}
	if !foundExact {
		t.Fatal("the exact unpruned tool output must be available to the summary/archive")
	}
	if plan.TokensBefore >= compactctx.EstimateTokens(model.history) {
		t.Fatalf("cut estimation should use the provider-facing pruned copy: plan=%d raw=%d", plan.TokensBefore, compactctx.EstimateTokens(model.history))
	}
}

func TestCompactionPlanCanForceOlderTurnsForRequestOverhead(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.history = []openai.Message{
		{Role: "user", Content: "old request"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "current request"},
	}
	if _, ok := model.compactionPlan(false); ok {
		t.Fatal("normal compaction should not summarize history that fits the recent-tail budget")
	}
	plan, ok := model.compactionPlan(true)
	if !ok {
		t.Fatal("forced compaction should summarize older turns when request overhead fills the window")
	}
	if plan.CutIndex != 2 || len(plan.Head) != 2 || len(plan.Tail) != 1 || plan.Tail[0].Content != "current request" {
		t.Fatalf("unexpected forced plan: %+v", plan)
	}
}
