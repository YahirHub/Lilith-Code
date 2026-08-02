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
	"github.com/lilith/li/internal/skills"
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

func TestRunDefaultAgentModelUsesParentTurnModel(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "commandcode", ParentModelID: "gpt-5.4",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}, {ID: "claude-haiku-4-5-20251001"}}}}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent:  agents.Agent{Name: "context7-docs", Description: "docs", Prompt: "Read docs.", Model: "default"},
		Prompt: "Get Baileys WhatsApp library docs", Description: "Get docs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("requests=%d", len(fake.requests))
	}
	if fake.requests[0].Provider.ID != "commandcode" || fake.requests[0].Model != "gpt-5.4" {
		t.Fatalf("request used %s/%s, want commandcode/gpt-5.4", fake.requests[0].Provider.ID, fake.requests[0].Model)
	}
}

func TestPlanParentForcesChildReadOnly(t *testing.T) {
	a := agents.Agent{Name: "worker", Description: "writes"}
	p := newToolPolicy(a, "plan", false)
	def, _ := tools.Get("create_file")
	if p.visible("create_file", def) {
		t.Fatal("write leaked through parent plan mode")
	}
}

func TestClaudeToolMapping(t *testing.T) {
	p := newToolPolicy(agents.Agent{Tools: []string{"Read", "Glob", "Grep"}}, "build", false)
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

func TestWorktreeIsolationRequiresRepository(t *testing.T) {
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent: agents.Agent{Name: "isolated", Isolation: "worktree"}, Prompt: "inspect",
	})
	if err == nil || !strings.Contains(err.Error(), "requires a git repository") {
		t.Fatalf("expected explicit worktree repository error, got %v", err)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("model should not start when worktree isolation cannot be honored")
	}
}

func TestOpenCodeLegacyDisabledToolsStayDenied(t *testing.T) {
	p := newToolPolicy(agents.Agent{ToolFlags: map[string]bool{"write": false, "bash": false}}, "build", false)
	for _, name := range []string{"create_file", "write_file", "append_file", "str_replace", "apply_diff", "run_terminal_command"} {
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

func TestResolveModelDefaultUsesParentSelection(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	cfg := providers.Config{Providers: []providers.Provider{
		{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}, {ID: "claude-haiku-4-5-20251001"}}},
	}}

	provider, model, err := resolveModel(cfg, "commandcode", "gpt-5.4", "default", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "commandcode" || model != "gpt-5.4" {
		t.Fatalf("resolved=%s/%s, want commandcode/gpt-5.4", provider.ID, model)
	}
}

func TestResolveModelExplicitOverridesParentSelection(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	cfg := providers.Config{Providers: []providers.Provider{
		{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}, {ID: "claude-haiku-4-5-20251001"}}},
	}}

	provider, model, err := resolveModel(cfg, "commandcode", "gpt-5.4", "haiku", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "commandcode" || model != "claude-haiku-4-5-20251001" {
		t.Fatalf("resolved=%s/%s, want commandcode/claude-haiku-4-5-20251001", provider.ID, model)
	}
}

func TestResolveModelCommaListUsesFirstResolvableCandidate(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	cfg := providers.Config{Providers: []providers.Provider{
		{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}}},
		{ID: "other", Name: "Other", Models: []providers.Model{{ID: "claude-sonnet-4-5"}}},
	}}

	provider, model, err := resolveModel(cfg, "commandcode", "gpt-5.4", "missing-model, other/claude-sonnet-4-5, default", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "other" || model != "claude-sonnet-4-5" {
		t.Fatalf("resolved=%s/%s, want other/claude-sonnet-4-5", provider.ID, model)
	}
}

func TestResolveModelCommaListCanFallBackToDefault(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	cfg := providers.Config{Providers: []providers.Provider{
		{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}}},
	}}

	provider, model, err := resolveModel(cfg, "commandcode", "gpt-5.4", "missing-model, default", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "commandcode" || model != "gpt-5.4" {
		t.Fatalf("resolved=%s/%s, want commandcode/gpt-5.4", provider.ID, model)
	}
}

func TestResolveModelInvocationDefaultOverridesPinnedAgent(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	cfg := providers.Config{Providers: []providers.Provider{
		{ID: "commandcode", Name: "CommandCode", Models: []providers.Model{{ID: "gpt-5.4"}, {ID: "claude-haiku-4-5-20251001"}}},
	}}

	provider, model, err := resolveModel(cfg, "commandcode", "gpt-5.4", "haiku", "default")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "commandcode" || model != "gpt-5.4" {
		t.Fatalf("resolved=%s/%s, want commandcode/gpt-5.4", provider.ID, model)
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

func TestWorktreeIncludePatternsAndShellGuard(t *testing.T) {
	patterns := parseIncludePatterns(".env\ncache/**\n!cache/tmp/**\n")
	if !includedByPatterns(".env", patterns) || !includedByPatterns("cache/data.bin", patterns) || includedByPatterns("cache/tmp/x", patterns) {
		t.Fatal("unexpected .worktreeinclude matching")
	}
	main := filepath.Join(t.TempDir(), "repo")
	if err := validateWorktreeCommand("git status --short", main); err != nil {
		t.Fatalf("safe git status blocked: %v", err)
	}
	if err := validateWorktreeCommand("git -C "+main+" status", main); err == nil {
		t.Fatal("git -C escape should be blocked")
	}
}

func TestWorktreeBaseRefHonorsTrustedClaudeSetting(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"worktree":{"baseRef":"head"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadWorktreeSettings(cfg, root, true).BaseRef; got != "head" {
		t.Fatalf("base ref=%q", got)
	}
}

func TestBackgroundPolicyUsesClaudeNarrowBuiltinSet(t *testing.T) {
	p := newToolPolicy(agents.Agent{}, "build", true)
	if !p.allowsName("read_files") || !p.allowsName("run_terminal_command") || !p.allowsName("Agent") {
		t.Fatal("background agent lost allowed core tools")
	}
	if p.allowsName("create_goal") {
		t.Fatal("background agent should not inherit unrelated built-in tools")
	}
}

func TestRunForkInheritsConversationAndDropsDanglingToolCall(t *testing.T) {
	fake := &fakeStreamer{}
	var dangling openai.ToolCall
	dangling.ID = "call-fork"
	dangling.Type = "function"
	dangling.Function.Name = "Agent"
	dangling.Function.Arguments = `{"subagent_type":"fork"}`
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
		ParentMessages: []openai.Message{
			{Role: "system", Content: "main system"},
			{Role: "user", Content: "original request"},
			{Role: "assistant", Content: "I will branch this.", ToolCalls: []openai.ToolCall{dangling}},
		},
		ParentToolNames: []string{"read_files"},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent:  agents.Agent{Name: "fork", Description: "branch", Model: "default"},
		Prompt: "inspect the alternative", Description: "inspect alternative", Fork: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("requests=%d", len(fake.requests))
	}
	request := fake.requests[0]
	if request.Provider.ID != "p" || request.Model != "m" {
		t.Fatalf("fork model=%s/%s, want p/m", request.Provider.ID, request.Model)
	}
	if len(request.Messages) != 4 {
		t.Fatalf("messages=%#v", request.Messages)
	}
	if !strings.HasPrefix(request.Messages[0].Content, "main system") || !strings.Contains(request.Messages[0].Content, "<code_intelligence>") || request.Messages[1].Content != "original request" {
		t.Fatalf("fork did not inherit parent context and code-intelligence profile: %#v", request.Messages)
	}
	if len(request.Messages[2].ToolCalls) != 0 || request.Messages[2].Content != "I will branch this." {
		t.Fatalf("dangling tool call was not sanitized: %#v", request.Messages[2])
	}
	if got := request.Messages[len(request.Messages)-1]; got.Role != "user" || got.Content != "inspect the alternative" {
		t.Fatalf("fork task=%#v", got)
	}
	if len(request.Tools) != 1 {
		t.Fatalf("fork tools=%d, want inherited read_files", len(request.Tools))
	}
}

func TestMergeCodeIntelSystemMessageDoesNotCreateUserTurn(t *testing.T) {
	messages := []openai.Message{{Role: "system", Content: "base"}, {Role: "user", Content: "task"}}
	merged := mergeCodeIntelSystemMessage(messages, "profile")
	if len(merged) != 2 {
		t.Fatalf("messages=%#v", merged)
	}
	if merged[0].Role != "system" || !strings.Contains(merged[0].Content, "<code_intelligence>") {
		t.Fatalf("profile was not merged into the system message: %#v", merged)
	}
	if messages[0].Content != "base" {
		t.Fatalf("parent messages were mutated: %#v", messages)
	}
	merged = mergeCodeIntelSystemMessage(merged, "profile")
	if strings.Count(merged[0].Content, "<code_intelligence>") != 1 {
		t.Fatalf("profile was duplicated: %#v", merged[0])
	}
}

func TestRunNamedSubagentStillIgnoresParentMessages(t *testing.T) {
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m",
		Providers:      providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
		ParentMessages: []openai.Message{{Role: "user", Content: "secret parent context"}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent:  agents.Agent{Name: "reviewer", Description: "reviews", Prompt: "Review only."},
		Prompt: "inspect auth", Description: "inspect auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range fake.requests[0].Messages {
		if strings.Contains(msg.Content, "secret parent context") {
			t.Fatalf("normal isolated child inherited parent context: %#v", fake.requests[0].Messages)
		}
	}
}

func TestRunRejectsRecursiveFork(t *testing.T) {
	fake := &fakeStreamer{}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m", ParentIsFork: true,
		Providers:      providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
		ParentMessages: []openai.Message{{Role: "system", Content: "main"}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{Agent: agents.Agent{Name: "fork"}, Prompt: "again", Fork: true})
	if err == nil || !strings.Contains(err.Error(), "cannot create another fork") {
		t.Fatalf("expected recursive fork error, got %v", err)
	}
	if len(fake.requests) != 0 {
		t.Fatal("recursive fork should fail before model request")
	}
}

func TestRunExpandsPluginRootInAgentAndPreloadedSkill(t *testing.T) {
	fake := &fakeStreamer{}
	pluginRoot := t.TempDir()
	skillDir := filepath.Join(pluginRoot, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: quality:review\ndescription: review\n---\nPlugin=${CLAUDE_PLUGIN_ROOT}\nSkill=${CLAUDE_SKILL_DIR}"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := []skills.Skill{{Name: "quality:review", FilePath: skillFile, BaseDir: skillDir, PluginRoot: pluginRoot}}
	cfg := Config{
		Client: fake, ConfigDir: t.TempDir(), Root: t.TempDir(), ParentProviderID: "p", ParentModelID: "m", Skills: catalog,
		Providers: providers.Config{Providers: []providers.Provider{{ID: "p", Name: "P", Models: []providers.Model{{ID: "m"}}}}},
	}
	_, err := Run(context.Background(), cfg, tools.AgentRequest{
		Agent: agents.Agent{
			Name: "quality:worker", Prompt: "Use ${CLAUDE_PLUGIN_ROOT}.", PluginRoot: pluginRoot,
			Skills: []string{"quality:review"},
		},
		Prompt: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	system := fake.requests[0].Messages[0].Content
	pluginSlash := filepath.ToSlash(pluginRoot)
	skillSlash := filepath.ToSlash(skillDir)
	if strings.Contains(system, "${CLAUDE_PLUGIN_ROOT}") || strings.Contains(system, "${CLAUDE_SKILL_DIR}") {
		t.Fatalf("plugin placeholders were not expanded: %q", system)
	}
	if !strings.Contains(system, "Use "+pluginSlash+".") || !strings.Contains(system, "Plugin="+pluginSlash) || !strings.Contains(system, "Skill="+skillSlash) {
		t.Fatalf("expanded plugin paths missing: %q", system)
	}
}

func TestCollectResetsPartialOutputAfterNetworkInterruption(t *testing.T) {
	ch := make(chan openai.Chunk, 8)
	ch <- openai.Chunk{Delta: "respuesta parcial"}
	ch <- openai.Chunk{Thinking: "razonamiento parcial"}
	ch <- openai.Chunk{Retry: &openai.RetryStatus{State: openai.ConnectivityOffline, Reset: true}}
	ch <- openai.Chunk{Delta: "respuesta final"}
	close(ch)

	var events []Event
	text, reasoning, calls, err := collect(ch, Config{
		Events: func(event Event) { events = append(events, event) },
	}, "task-retry", tools.AgentRequest{Agent: agents.Agent{Name: "worker"}}, "model", 1)
	if err != nil {
		t.Fatal(err)
	}
	if text != "respuesta final" || reasoning != "" || len(calls) != 0 {
		t.Fatalf("partial attempt survived retry reset: text=%q reasoning=%q calls=%#v", text, reasoning, calls)
	}
	foundReset := false
	for _, event := range events {
		if event.Kind == EventStreamReset {
			foundReset = true
			break
		}
	}
	if !foundReset {
		t.Fatalf("missing stream reset event: %#v", events)
	}
}
