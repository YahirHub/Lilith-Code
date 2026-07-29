// Package subagents runs Claude-compatible subagents in fresh isolated model
// contexts while reusing Lilith's providers and tool registry.
package subagents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/agents"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/skills"
	litodo "github.com/lilith/li/internal/todo"
	"github.com/lilith/li/internal/tools"
)

// Streamer makes the runtime testable without constructing an HTTP server.
type Streamer interface {
	Stream(ctx context.Context, req openai.Request) <-chan openai.Chunk
}

// Config is inherited from the parent session. ParentMessages are
// intentionally absent: normal subagents start fresh and receive only the
// delegated task, matching Claude/OpenCode/Pi isolated-worker semantics.
type Config struct {
	Client           Streamer
	Providers        providers.Config
	ConfigDir        string
	Root             string
	ParentProviderID string
	ParentModelID    string
	ParentMode       planstate.Mode
	Skills           []skills.Skill
	Agents           []agents.Agent
	// Depth is 1 for a top-level subagent. MaxDepth follows Claude Code's
	// current default of three subagent layers below the main conversation.
	// LILITH_MAX_SUBAGENT_DEPTH or the Claude-compatible
	// CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH can override the default.
	Depth    int
	MaxDepth int

	// ParentTaskID links nested workers for UI/orchestration. Events is an
	// optional live progress stream; nested workers inherit it.
	ParentTaskID string
	Events       EventSink
}

func Run(ctx context.Context, cfg Config, req tools.AgentRequest) (tools.AgentResult, error) {
	if cfg.Client == nil {
		return tools.AgentResult{}, errors.New("subagent client unavailable")
	}
	if strings.TrimSpace(cfg.Root) == "" {
		return tools.AgentResult{}, errors.New("subagent project root is empty")
	}
	if strings.EqualFold(strings.TrimSpace(req.Agent.Isolation), "worktree") {
		return tools.AgentResult{}, fmt.Errorf("agent %s requests isolation: worktree, which Lilith does not implement yet", req.Agent.Name)
	}
	provider, model, err := resolveModel(cfg.Providers, cfg.ParentProviderID, cfg.ParentModelID, req.Agent.Model, req.Model)
	if err != nil {
		return tools.AgentResult{}, err
	}

	depth := cfg.Depth
	if depth <= 0 {
		depth = 1
	}
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = configuredMaxDepth()
	}

	store := newChildStore(cfg.ConfigDir, cfg.Root)
	resumed := false
	var child *childSession
	if strings.TrimSpace(req.TaskID) != "" {
		child, err = store.load(req.TaskID)
		if err != nil {
			return tools.AgentResult{}, fmt.Errorf("resume subagent %s: %w", req.TaskID, err)
		}
		if !strings.EqualFold(child.AgentName, req.Agent.Name) {
			return tools.AgentResult{}, fmt.Errorf("task %s belongs to agent %s, not %s", child.ID, child.AgentName, req.Agent.Name)
		}
		if filepathClean(child.Project) != filepathClean(cfg.Root) {
			return tools.AgentResult{}, fmt.Errorf("task %s belongs to a different project", child.ID)
		}
		resumed = true
		child.ParentTaskID = cfg.ParentTaskID
		child.Description = req.Description
		child.Depth = depth
		child.Status = "running"
		child.FinishedAt = time.Time{}
		providerFromStore := cfg.Providers.FindProvider(child.ProviderID)
		if providerFromStore != nil {
			provider = *providerFromStore
			model = child.ModelID
		}
		child.Messages = append(child.Messages, openai.Message{Role: "user", Content: req.Prompt})
	} else {
		now := timeNow()
		child = &childSession{
			ID: newTaskID(), AgentName: req.Agent.Name, ParentTaskID: cfg.ParentTaskID, Description: req.Description, Depth: depth, Status: "running", Project: filepathClean(cfg.Root),
			ProviderID: provider.ID, ModelID: model, CreatedAt: now, UpdatedAt: now,
			Messages: []openai.Message{
				{Role: "system", Content: buildSystemPrompt(req.Agent, cfg.Root, cfg.Skills, cfg.Agents, depth < maxDepth)},
				{Role: "user", Content: req.Prompt},
			},
		}
	}

	localTodos := litodo.NewManager(nil)
	mode := planstate.Build
	if cfg.ParentMode == planstate.Plan {
		mode = planstate.Plan
	} else if strings.EqualFold(strings.TrimSpace(req.Agent.PermissionMode), "plan") {
		mode = planstate.Plan
	}

	policy := newToolPolicy(req.Agent, mode)
	env := tools.Env{Root: cfg.Root, ConfigDir: cfg.ConfigDir, Skills: cfg.Skills, Todos: localTodos, AgentMode: mode}
	if depth < maxDepth && len(cfg.Agents) > 0 {
		env.Agents = cfg.Agents
		env.RunAgent = func(childCtx context.Context, childReq tools.AgentRequest) (tools.AgentResult, error) {
			nested := cfg
			nested.ParentProviderID = provider.ID
			nested.ParentModelID = model
			nested.ParentMode = mode
			nested.Depth = depth + 1
			nested.MaxDepth = maxDepth
			nested.ParentTaskID = child.ID
			return Run(childCtx, nested, childReq)
		}
	}
	env.ToolVisible = func(name string, def tools.Definition) bool { return policy.visible(name, def) }
	env.ValidateTool = func(name string, def tools.Definition, args map[string]any) error {
		return policy.validate(name, def, args)
	}

	active := append([]string(nil), child.Tools...)
	if len(active) == 0 {
		active = policy.initialTools(req.Prompt, env)
	}
	active = tools.FilterAvailable(active, env)
	child.Tools = append([]string(nil), active...)
	if err := store.save(child); err != nil {
		return tools.AgentResult{}, err
	}
	emit(cfg, Event{
		Kind: EventStarted, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID,
		AgentName: req.Agent.Name, Description: req.Description, Model: model,
		Depth: depth, Resumed: resumed, At: timeNow(),
	})

	turns := 0
	for {
		if err := ctx.Err(); err != nil {
			child.Status = "killed"
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: EventCanceled, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: err.Error(), At: timeNow()})
			return tools.AgentResult{}, err
		}
		turns++
		if req.Agent.MaxTurns > 0 && turns > req.Agent.MaxTurns {
			err := fmt.Errorf("subagent %s reached its configured maxTurns=%d", req.Agent.Name, req.Agent.MaxTurns)
			child.Status = "failed"
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: EventFailed, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: err.Error(), At: timeNow()})
			return tools.AgentResult{}, err
		}

		var schemas []any
		for _, schema := range tools.Schemas(active) {
			schemas = append(schemas, schema)
		}
		ch := cfg.Client.Stream(ctx, openai.Request{Provider: provider, Model: model, Messages: child.Messages, Stream: true, Tools: schemas})
		text, reasoning, calls, err := collect(ch, cfg, child.ID, req, model, depth)
		if err != nil {
			wrapped := fmt.Errorf("subagent %s: %w", req.Agent.Name, err)
			kind := EventFailed
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				kind = EventCanceled
				child.Status = "killed"
			} else {
				child.Status = "failed"
			}
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: kind, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: wrapped.Error(), At: timeNow()})
			return tools.AgentResult{}, wrapped
		}
		assistant := openai.Message{Role: "assistant", Content: text, ReasoningContent: reasoning, ToolCalls: calls}
		child.Messages = append(child.Messages, assistant)
		if len(calls) == 0 {
			child.Status = "completed"
			child.FinishedAt = timeNow()
			if err := store.save(child); err != nil {
				return tools.AgentResult{}, err
			}
			emit(cfg, Event{Kind: EventCompleted, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: strings.TrimSpace(text), At: timeNow()})
			return tools.AgentResult{TaskID: child.ID, AgentName: req.Agent.Name, Text: strings.TrimSpace(text), Resumed: resumed}, nil
		}

		materialized := append([]string(nil), active...)
		env.Materialize = func(names []string) {
			for _, name := range names {
				if policy.allowsName(name) {
					materialized = appendUnique(materialized, name)
				}
			}
		}
		toolMessages := executeToolCalls(ctx, cfg, child.ID, req, model, depth, calls, env)
		child.Messages = append(child.Messages, toolMessages...)
		active = tools.FilterAvailable(uniqueSorted(materialized), env)
		child.Tools = append([]string(nil), active...)
		if err := store.save(child); err != nil {
			return tools.AgentResult{}, err
		}
	}
}

func collect(ch <-chan openai.Chunk, cfg Config, taskID string, req tools.AgentRequest, model string, depth int) (text, reasoning string, calls []openai.ToolCall, err error) {
	var tb, rb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return "", "", nil, chunk.Err
		}
		if chunk.Delta != "" {
			tb.WriteString(chunk.Delta)
			emit(cfg, Event{Kind: EventText, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: chunk.Delta, At: timeNow()})
		}
		if chunk.Thinking != "" {
			rb.WriteString(chunk.Thinking)
			emit(cfg, Event{Kind: EventThinking, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Content: chunk.Thinking, At: timeNow()})
		}
		if len(chunk.ToolCalls) > 0 && !chunk.Partial {
			calls = append([]openai.ToolCall(nil), chunk.ToolCalls...)
		}
	}
	return tb.String(), rb.String(), calls, nil
}

func executeToolCalls(ctx context.Context, cfg Config, taskID string, req tools.AgentRequest, model string, depth int, calls []openai.ToolCall, env tools.Env) []openai.Message {
	results := make([]openai.Message, len(calls))
	execute := func(i int, call openai.ToolCall) {
		args := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				out := "error: invalid JSON arguments: " + err.Error()
				emitTool(cfg, EventToolStarted, taskID, req, model, depth, call, "")
				emitTool(cfg, EventToolFinished, taskID, req, model, depth, call, out)
				results[i] = toolMessage(call, out)
				return
			}
		}
		emitTool(cfg, EventToolStarted, taskID, req, model, depth, call, "")
		out, execErr := tools.Execute(ctx, call.Function.Name, args, env)
		if execErr != nil {
			out = "error: " + execErr.Error()
		}
		emitTool(cfg, EventToolFinished, taskID, req, model, depth, call, out)
		results[i] = toolMessage(call, out)
	}

	// Claude/OpenCode can orchestrate independent child workers in parallel. If
	// a subagent emits a pure Agent batch, run those children concurrently while
	// preserving result order for the provider protocol.
	if len(calls) > 1 && allAgentToolCalls(calls) {
		var wg sync.WaitGroup
		for i, call := range calls {
			i, call := i, call
			wg.Add(1)
			go func() {
				defer wg.Done()
				execute(i, call)
			}()
		}
		wg.Wait()
		return results
	}
	for i, call := range calls {
		execute(i, call)
	}
	return results
}

func emit(cfg Config, event Event) {
	if cfg.Events != nil {
		cfg.Events(event)
	}
}

func emitTool(cfg Config, kind EventKind, taskID string, req tools.AgentRequest, model string, depth int, call openai.ToolCall, content string) {
	emit(cfg, Event{Kind: kind, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, ToolCallID: call.ID, ToolName: call.Function.Name, ToolArgs: call.Function.Arguments, Content: content, At: timeNow()})
}

func allAgentToolCalls(calls []openai.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		name := strings.ToLower(strings.TrimSpace(call.Function.Name))
		if name != "agent" && name != "task" {
			return false
		}
	}
	return true
}

func toolMessage(call openai.ToolCall, content string) openai.Message {
	return openai.Message{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: content}
}

func buildSystemPrompt(a agents.Agent, root string, catalog []skills.Skill, agentCatalog []agents.Agent, canDelegate bool) string {
	var b strings.Builder
	prompt := strings.TrimSpace(a.Prompt)
	if prompt == "" {
		prompt = "Complete the delegated task accurately and return a concise final result."
	}
	b.WriteString(prompt)
	b.WriteString("\n\nSubagent environment:\n- Working directory: ")
	b.WriteString(filepathSlash(root))
	b.WriteString("\n- You are an isolated subagent. You do not have the parent conversation history. The user-facing parent receives only your final result.\n")
	b.WriteString("- Do not ask the parent to repeat information already present in the delegated task. Investigate with available tools when possible.\n")
	if canDelegate {
		if available := agents.FormatForPrompt(agentCatalog); available != "" {
			b.WriteString(available)
		}
	}
	if preloaded := preloadSkills(a.Skills, catalog); preloaded != "" {
		b.WriteString(preloaded)
	}
	return b.String()
}

func preloadSkills(names []string, catalog []skills.Skill) string {
	if len(names) == 0 || len(catalog) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range names {
		sk := skills.Find(catalog, name)
		if sk == nil {
			continue
		}
		body, err := skills.ReadContent(*sk)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n\n<preloaded_skill name=\"%s\">\n%s\n</preloaded_skill>", sk.Name, strings.TrimSpace(body))
	}
	return b.String()
}

type toolPolicy struct {
	mode  planstate.Mode
	allow map[string]bool // nil means inherit all
	deny  map[string]bool
}

func newToolPolicy(a agents.Agent, mode planstate.Mode) toolPolicy {
	p := toolPolicy{mode: mode, deny: map[string]bool{"plan_question": true, "plan_exit": true}}
	if len(a.Tools) > 0 {
		p.allow = map[string]bool{}
		for _, external := range a.Tools {
			for _, name := range mapExternalTool(external) {
				p.allow[name] = true
			}
		}
	}
	for _, external := range a.DisallowedTools {
		for _, name := range mapExternalTool(external) {
			p.deny[name] = true
		}
	}
	for external, enabled := range a.ToolFlags {
		if enabled {
			continue
		}
		for _, name := range mapExternalTool(external) {
			p.deny[name] = true
		}
	}
	for key, action := range a.Permissions {
		if action != "deny" && action != "ask" {
			continue
		}
		for _, name := range mapPermissionKey(key) {
			p.deny[name] = true
		}
	}
	return p
}

func (p toolPolicy) visible(name string, def tools.Definition) bool {
	if p.deny[name] {
		return false
	}
	if p.allow != nil && !p.allow[name] {
		return false
	}
	return planstate.ToolVisible(p.mode, name, def.Mutating)
}
func (p toolPolicy) validate(name string, def tools.Definition, args map[string]any) error {
	if !p.visible(name, def) {
		return fmt.Errorf("tool %s is not allowed for this subagent", name)
	}
	return planstate.ValidateTool(p.mode, name, def.Mutating, args)
}
func (p toolPolicy) allowsName(name string) bool {
	def, ok := tools.Get(name)
	return ok && p.visible(name, def)
}
func (p toolPolicy) initialTools(prompt string, env tools.Env) []string {
	if p.allow != nil {
		var names []string
		for name := range p.allow {
			if p.allowsName(name) {
				names = append(names, name)
			}
		}
		return uniqueSorted(names)
	}
	names := tools.SelectAvailable(prompt, env)
	if len(env.Skills) > 0 {
		names = tools.WithSkillTools(names, true)
	}
	// A Claude-style inherited agent can orchestrate children whenever the
	// runtime exposes Agent at this depth. Do not leave orchestration to lexical
	// lazy-tool selection: the worker's own system prompt explicitly advertises
	// the roster, so Agent must be in the schema from its first turn. Agents that
	// declare an explicit tools allowlist still control this via p.allow above.
	if env.RunAgent != nil && p.allowsName("Agent") {
		names = append(names, "Agent")
	}
	// An inherited Claude agent has access to all tools, but Lilith keeps the
	// rest of its registry lazy: tool_search can materialize any policy-allowed
	// capability without bloating every child request.
	return uniqueSorted(names)
}

func mapExternalTool(raw string) []string {
	v := strings.TrimSpace(raw)
	if i := strings.IndexByte(v, '('); i > 0 {
		v = v[:i]
	}
	switch strings.ToLower(v) {
	case "read", "read_files":
		return []string{"read_files"}
	case "glob":
		return []string{"glob"}
	case "grep", "search", "code_search":
		return []string{"code_search"}
	case "list", "list_directory":
		return []string{"list_directory"}
	case "bash", "powershell", "run_terminal_command":
		return []string{"run_terminal_command"}
	case "edit":
		return []string{"str_replace", "apply_diff"}
	case "write":
		return []string{"create_file", "str_replace", "apply_diff"}
	case "webfetch", "read_url":
		return []string{"read_url"}
	case "websearch", "web_search":
		return []string{"web_search"}
	case "todowrite", "todo_write":
		return []string{"todo_write"}
	case "skill":
		return []string{"list_skills", "skill_read", "skill_search", "skill_files"}
	case "toolsearch", "tool_search":
		return []string{"tool_search"}
	case "agent", "task":
		return []string{"Agent"}
	default:
		if _, ok := tools.Get(v); ok {
			return []string{v}
		}
		if _, ok := tools.Get(strings.ToLower(v)); ok {
			return []string{strings.ToLower(v)}
		}
		return nil
	}
}

func mapPermissionKey(key string) []string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "read":
		return []string{"read_files"}
	case "edit":
		return []string{"create_file", "str_replace", "apply_diff"}
	case "glob":
		return []string{"glob"}
	case "grep":
		return []string{"code_search"}
	case "list":
		return []string{"list_directory"}
	case "bash":
		return []string{"run_terminal_command"}
	case "task", "agent":
		return []string{"Agent"}
	case "todowrite":
		return []string{"todo_write"}
	case "webfetch":
		return []string{"read_url"}
	case "websearch":
		return []string{"web_search"}
	case "skill":
		return []string{"list_skills", "skill_read", "skill_search", "skill_files"}
	default:
		return []string{key}
	}
}

func configuredMaxDepth() int {
	for _, key := range []string{"LILITH_MAX_SUBAGENT_DEPTH", "CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 3
}

func resolveModel(cfg providers.Config, parentProvider, parentModel, agentModel, invocationModel string) (providers.Provider, string, error) {
	want := strings.TrimSpace(invocationModel)
	if want == "" {
		want = strings.TrimSpace(agentModel)
	}
	if want == "" || strings.EqualFold(want, "inherit") {
		p := cfg.FindProvider(parentProvider)
		if p == nil {
			return providers.Provider{}, "", errors.New("parent provider not found")
		}
		return *p, parentModel, nil
	}
	if strings.Contains(want, "/") {
		parts := strings.SplitN(want, "/", 2)
		if p := cfg.FindProvider(parts[0]); p != nil && providerHasModel(*p, parts[1]) {
			return *p, parts[1], nil
		}
	}
	// Claude aliases are resolved pragmatically against configured model IDs.
	needle := strings.ToLower(want)
	if p := cfg.FindProvider(parentProvider); p != nil {
		if model := findModel(*p, needle); model != "" {
			return *p, model, nil
		}
	}
	for _, p := range cfg.Providers {
		if model := findModel(p, needle); model != "" {
			return p, model, nil
		}
	}
	// Portable Claude agents often pin sonnet/opus/haiku. On a Lilith setup
	// backed only by DeepSeek/Qwen/etc. there is no equivalent alias; inheriting
	// the parent is more useful than making an otherwise portable agent fail.
	if needle == "sonnet" || needle == "opus" || needle == "haiku" {
		if p := cfg.FindProvider(parentProvider); p != nil {
			return *p, parentModel, nil
		}
	}
	return providers.Provider{}, "", fmt.Errorf("subagent model %q is not configured; use inherit or provider/model", want)
}
func findModel(p providers.Provider, want string) string {
	for _, m := range p.Models {
		if strings.EqualFold(m.ID, want) || strings.EqualFold(m.Name, want) {
			return m.ID
		}
	}
	if want == "sonnet" || want == "opus" || want == "haiku" {
		for _, m := range p.Models {
			if strings.Contains(strings.ToLower(m.ID+" "+m.Name), want) {
				return m.ID
			}
		}
	}
	return ""
}
func providerHasModel(p providers.Provider, id string) bool {
	for _, m := range p.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}
func appendUnique(dst []string, value string) []string {
	for _, v := range dst {
		if v == value {
			return dst
		}
	}
	return append(dst, value)
}
func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
