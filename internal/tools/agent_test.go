package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/lilith/li/internal/agents"
)

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
