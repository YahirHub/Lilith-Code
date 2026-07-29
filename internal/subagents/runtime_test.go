package subagents

import (
	"context"
	"strings"
	"testing"

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/tools"
)

type fakeStreamer struct{ requests []openai.Request }

func (f *fakeStreamer) Stream(_ context.Context, req openai.Request) <-chan openai.Chunk {
	f.requests = append(f.requests, req)
	ch := make(chan openai.Chunk, 2)
	ch <- openai.Chunk{Delta: "isolated result"}
	ch <- openai.Chunk{Done: true}
	close(ch)
	return ch
}

func TestRunStartsFreshIsolatedContext(t *testing.T) {
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
	}
	result, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent:  agents.Agent{Name: "reviewer", Description: "reviews", Prompt: "You review."},
		Prompt: "inspect auth", Description: "inspect auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "isolated result" || result.TaskID == "" {
		t.Fatalf("result=%#v", result)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("requests=%d", len(fake.requests))
	}
	msgs := fake.requests[0].Messages
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("messages=%#v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "You review.") || msgs[1].Content != "inspect auth" {
		t.Fatalf("messages=%#v", msgs)
	}
}

func TestPlanParentForcesChildReadOnly(t *testing.T) {
	a := agents.Agent{Name: "worker", Description: "writes"}
	p := newToolPolicy(a, "plan")
	def, _ := tools.Get("create_file")
	if p.visible("create_file", def) {
		t.Fatal("write leaked through parent plan mode")
	}
}

func TestClaudeToolMapping(t *testing.T) {
	p := newToolPolicy(agents.Agent{Tools: []string{"Read", "Glob", "Grep"}}, "build")
	for _, name := range []string{"read_files", "glob", "code_search"} {
		def, _ := tools.Get(name)
		if !p.visible(name, def) {
			t.Fatalf("%s should be visible", name)
		}
	}
	def, _ := tools.Get("create_file")
	if p.visible("create_file", def) {
		t.Fatal("create_file should not be visible")
	}
}

func TestWorktreeIsolationFailsClosed(t *testing.T) {
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent: agents.Agent{Name: "isolated", Isolation: "worktree"}, Prompt: "inspect",
	})
	if err == nil || !strings.Contains(err.Error(), "isolation: worktree") {
		t.Fatalf("expected explicit worktree isolation error, got %v", err)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("model should not start when worktree isolation cannot be honored")
	}
}

func TestOpenCodeLegacyDisabledToolsStayDenied(t *testing.T) {
	p := newToolPolicy(agents.Agent{ToolFlags: map[string]bool{"write": false, "bash": false}}, "build")
	for _, name := range []string{"create_file", "str_replace", "apply_diff", "run_terminal_command"} {
		def, _ := tools.Get(name)
		if p.visible(name, def) {
			t.Fatalf("%s should be denied by legacy OpenCode tools map", name)
		}
	}
}

func TestNestedAgentAvailableUntilDepthLimit(t *testing.T) {
	a := agents.Agent{Name: "general-purpose", Description: "worker"}
	catalog := []agents.Agent{a}
	fake := &fakeStreamer{}
	cfg := Config{Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), Agents: catalog, Depth: 1, MaxDepth: 3}
	env := tools.Env{Agents: cfg.Agents}
	if cfg.Depth >= cfg.MaxDepth {
		t.Fatal("test setup invalid")
	}
	// Runtime wiring supplies RunAgent while below the depth ceiling, which is
	// the availability gate used by the canonical Agent tool.
	env.RunAgent = func(context.Context, tools.AgentRequest) (tools.AgentResult, error) { return tools.AgentResult{}, nil }
	def, _ := tools.Get("Agent")
	if def.Available == nil || !def.Available(env) {
		t.Fatal("Agent should be available below nesting depth")
	}
	env.RunAgent = nil
	if def.Available(env) {
		t.Fatal("Agent should disappear at nesting depth when runtime callback is withheld")
	}
}

func TestConfiguredMaxDepthDefaultsToClaudeValue(t *testing.T) {
	t.Setenv("LILITH_MAX_SUBAGENT_DEPTH", "")
	t.Setenv("CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH", "")
	if got := configuredMaxDepth(); got != 3 {
		t.Fatalf("max depth=%d, want 3", got)
	}
	t.Setenv("CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH", "2")
	if got := configuredMaxDepth(); got != 2 {
		t.Fatalf("Claude-compatible max depth=%d, want 2", got)
	}
}
