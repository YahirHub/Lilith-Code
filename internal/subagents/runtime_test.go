package subagents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

type scriptedStreamer struct {
	responses [][]openai.Chunk
	requests  int
}

func (s *scriptedStreamer) Stream(_ context.Context, _ openai.Request) <-chan openai.Chunk {
	idx := s.requests
	s.requests++
	ch := make(chan openai.Chunk, 8)
	if idx < len(s.responses) {
		for _, chunk := range s.responses[idx] {
			ch <- chunk
		}
	}
	close(ch)
	return ch
}

func TestRunEmitsObservableLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	var call openai.ToolCall
	call.ID = "call-read"
	call.Type = "function"
	call.Function.Name = "read_files"
	call.Function.Arguments = `{"paths":["hello.txt"]}`
	fake := &scriptedStreamer{responses: [][]openai.Chunk{
		{{Thinking: "inspect"}, {ToolCalls: []openai.ToolCall{call}}},
		{{Delta: "done"}},
	}}
	var events []Event
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: root, ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
		Events:    func(event Event) { events = append(events, event) },
	}
	result, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent:  agents.Agent{Name: "explore", Description: "explores", Tools: []string{"Read"}},
		Prompt: "inspect", Description: "inspect file",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "done" {
		t.Fatalf("result=%#v", result)
	}
	var kinds []EventKind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	want := []EventKind{EventStarted, EventThinking, EventToolStarted, EventToolFinished, EventText, EventCompleted}
	for _, kind := range want {
		found := false
		for _, got := range kinds {
			if got == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing event %q in %#v", kind, kinds)
		}
	}
	if events[0].TaskID == "" || events[0].Depth != 1 {
		t.Fatalf("started=%#v", events[0])
	}
}

func TestChildPromptAdvertisesAgentsOnlyBelowNestingLimit(t *testing.T) {
	catalog := []agents.Agent{
		{Name: "worker", Description: "general worker", Prompt: "Work."},
		{Name: "Explore", Description: "read-only exploration", Prompt: "Explore."},
	}
	providerCfg := providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}}

	below := &fakeStreamer{}
	_, err := Run(context.Background(), Config{
		Client: below, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providerCfg, Agents: catalog, Depth: 1, MaxDepth: 3,
	}, tools.AgentRequest{Agent: catalog[0], Prompt: "delegate if useful"})
	if err != nil {
		t.Fatal(err)
	}
	if len(below.requests) != 1 {
		t.Fatalf("requests=%d", len(below.requests))
	}
	system := below.requests[0].Messages[0].Content
	if !strings.Contains(system, "<available_agents>") || !strings.Contains(system, "Explore") {
		t.Fatalf("nested-capable child did not receive agent catalog: %q", system)
	}
	toolJSON, err := json.Marshal(below.requests[0].Tools)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toolJSON), `"name":"Agent"`) {
		t.Fatalf("nested-capable child did not receive Agent tool schema: %s", toolJSON)
	}

	atLimit := &fakeStreamer{}
	_, err = Run(context.Background(), Config{
		Client: atLimit, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providerCfg, Agents: catalog, Depth: 3, MaxDepth: 3,
	}, tools.AgentRequest{Agent: catalog[0], Prompt: "do not delegate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(atLimit.requests) != 1 {
		t.Fatalf("requests=%d", len(atLimit.requests))
	}
	if strings.Contains(atLimit.requests[0].Messages[0].Content, "<available_agents>") {
		t.Fatal("agent catalog leaked into child at nesting ceiling")
	}
	toolJSON, err = json.Marshal(atLimit.requests[0].Tools)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(toolJSON), `"name":"Agent"`) {
		t.Fatalf("Agent tool leaked into child at nesting ceiling: %s", toolJSON)
	}
}

func TestLifecycleCarriesParentTaskID(t *testing.T) {
	fake := &fakeStreamer{}
	var started Event
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentTaskID: "agent-parent", Depth: 2, MaxDepth: 3,
		ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
		Events: func(event Event) {
			if event.Kind == EventStarted {
				started = event
			}
		},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent: agents.Agent{Name: "child", Description: "child"}, Prompt: "work", Description: "nested work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ParentTaskID != "agent-parent" || started.Depth != 2 || started.TaskID == "" {
		t.Fatalf("started=%#v", started)
	}
}
