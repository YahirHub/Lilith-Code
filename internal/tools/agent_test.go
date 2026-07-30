package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/lilith/li/internal/agents"
)

func TestAgentToolAcceptsConversationForkWithoutCatalogEntry(t *testing.T) {
	var captured AgentRequest
	env := Env{RunAgent: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		captured = req
		return AgentResult{TaskID: "task-1", AgentName: req.Agent.Name, Text: "done", Background: req.Background}, nil
	}}
	out, err := Execute(context.Background(), "Agent", map[string]any{
		"description":       "inspect alternative",
		"prompt":            "inspect the alternative",
		"subagent_type":     "fork",
		"run_in_background": true,
		"isolation":         "worktree",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !captured.Fork || captured.Agent.Name != "fork" || !captured.Background || captured.Isolation != "worktree" {
		t.Fatalf("captured=%#v", captured)
	}
	if !strings.Contains(out, `state="running"`) {
		t.Fatalf("output=%s", out)
	}
}

func TestAgentToolRespectsForkDisableEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CODE_FORK_SUBAGENT", "0")
	env := Env{RunAgent: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		t.Fatal("disabled fork should not reach runtime")
		return AgentResult{}, nil
	}}
	_, err := Execute(context.Background(), "Agent", map[string]any{
		"description":   "inspect alternative",
		"prompt":        "inspect",
		"subagent_type": "fork",
	}, env)
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_FORK_SUBAGENT=0") {
		t.Fatalf("expected disabled fork error, got %v", err)
	}
}

func TestAgentToolDelegatesAndAliasesTask(t *testing.T) {
	env := Env{
		Agents: []agents.Agent{{Name: "reviewer", Description: "reviews", Prompt: "Review."}},
		RunAgent: func(_ context.Context, req AgentRequest) (AgentResult, error) {
			if req.Agent.Name != "reviewer" || req.Prompt != "check this" {
				t.Fatalf("request=%#v", req)
			}
			return AgentResult{TaskID: "agent-1", AgentName: "reviewer", Text: "done"}, nil
		},
	}
	args := map[string]any{"description": "review code", "prompt": "check this", "subagent_type": "reviewer"}
	out, err := Execute(context.Background(), "Task", args, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `id="agent-1"`) || !strings.Contains(out, "done") {
		t.Fatalf("output=%s", out)
	}
}

func TestAgentUnavailableWithoutRuntime(t *testing.T) {
	if _, err := Execute(context.Background(), "Agent", map[string]any{"description": "x", "prompt": "x", "subagent_type": "x"}, Env{}); err == nil {
		t.Fatal("expected unavailable")
	}
}
