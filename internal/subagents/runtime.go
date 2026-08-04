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
	"github.com/lilith/li/internal/codeintel"
	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/hooks"
	"github.com/lilith/li/internal/instructions"
	"github.com/lilith/li/internal/mcp"
	limemory "github.com/lilith/li/internal/memory"
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

// Config is inherited from the parent session. Normal named subagents ignore
// ParentMessages and start fresh; Claude-compatible forks explicitly opt into
// inheriting this model-facing snapshot and the active lazy-tool set.
type Config struct {
	Client    Streamer
	Providers providers.Config
	ConfigDir string
	Root      string
	// StoreProject keeps child sessions grouped under the original project even
	// when execution moves into an isolated git worktree.
	StoreProject     string
	ParentProviderID string
	ParentModelID    string
	ParentMode       planstate.Mode
	ParentMessages   []openai.Message
	ParentToolNames  []string
	// ParentIsFork prevents recursive conversation forks while still allowing a
	// forked worker to delegate normal isolated subagents.
	ParentIsFork bool
	Skills       []skills.Skill
	Agents       []agents.Agent
	// CodeIntel shares the parent repository index when roots match. Worktree
	// subagents transparently receive their own persistent index.
	CodeIntel *codeintel.Manager
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
	// BackgroundContext outlives a single parent turn and is used for detached
	// children. Nested runtimes inherit it so cancellation at session shutdown
	// still tears down the whole background tree.
	BackgroundContext context.Context
	// ParentMCP is the already-connected main-session MCP runtime. Claude
	// subagents inherit parent MCP tools without reconnecting, while their
	// mcpServers frontmatter may add child-only inline servers.
	ParentMCP *mcp.Runtime
	// PluginHooks carries active plugin-owned lifecycle hooks. Standard
	// user/project hooks are loaded directly in the child so their cwd follows
	// worktree isolation without duplicating entries from the parent.
	PluginHooks *hooks.Runner
}

func StartBackground(cfg Config, req tools.AgentRequest) (tools.AgentResult, error) {
	ctx := cfg.BackgroundContext
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.AllocatedTaskID) == "" {
		req.AllocatedTaskID = newTaskID()
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		taskID = req.AllocatedTaskID
	}
	req.Background = true

	// Reserve the task id before returning it to the caller. Otherwise two
	// detached resumes can both report "running" and the loser may later emit a
	// failed event with the same task id, corrupting the live panel of the winner.
	var releaseTask func()
	taskLeaseHeld := false
	if strings.TrimSpace(cfg.Root) != "" {
		storeProject := strings.TrimSpace(cfg.StoreProject)
		if storeProject == "" {
			storeProject = cfg.Root
		}
		var acquireErr error
		releaseTask, acquireErr = newChildStore(cfg.ConfigDir, storeProject).acquire(taskID)
		if acquireErr != nil {
			return tools.AgentResult{}, acquireErr
		}
		taskLeaseHeld = true
	}

	// Run can fail before it creates/persists the child (model resolution,
	// worktree setup, MCP startup, hooks, etc.). A detached caller has already
	// received a task id at that point, so guarantee one terminal lifecycle
	// event instead of leaving the parent panel stuck in "running" forever.
	originalEvents := cfg.Events
	var terminalMu sync.Mutex
	terminalSeen := false
	cfg.Events = func(event Event) {
		if event.TaskID == taskID && IsTerminalEvent(event.Kind) {
			terminalMu.Lock()
			terminalSeen = true
			terminalMu.Unlock()
		}
		if originalEvents != nil {
			originalEvents(event)
		}
	}
	go func() {
		if releaseTask != nil {
			defer releaseTask()
		}
		_, runErr := run(ctx, cfg, req, taskLeaseHeld)
		if runErr == nil {
			return
		}
		terminalMu.Lock()
		alreadyTerminal := terminalSeen
		if !alreadyTerminal {
			terminalSeen = true
		}
		terminalMu.Unlock()
		if alreadyTerminal {
			return
		}
		depth := cfg.Depth
		if depth <= 0 {
			depth = 1
		}
		kind := EventFailed
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			kind = EventCanceled
		}
		if originalEvents != nil {
			originalEvents(Event{
				Kind: kind, TaskID: taskID, ParentTaskID: cfg.ParentTaskID,
				AgentName: req.Agent.Name, Description: req.Description, Depth: depth,
				Resumed: strings.TrimSpace(req.TaskID) != "", Background: true,
				Content: runErr.Error(), At: timeNow(),
			})
		}
	}()
	return tools.AgentResult{TaskID: taskID, AgentName: req.Agent.Name, Text: "Subagente iniciado en background. El resultado llegará como notificación cuando termine.", Background: true, Resumed: strings.TrimSpace(req.TaskID) != ""}, nil
}

// Dispatch applies the host's background policy consistently. When detached
// tasks are disabled, the request becomes a true foreground run: it uses
// foreground permissions, emits foreground events and returns the final text.
func Dispatch(ctx context.Context, cfg Config, req tools.AgentRequest, allowBackground bool) (tools.AgentResult, error) {
	if req.Background && allowBackground {
		return StartBackground(cfg, req)
	}
	req.Background = false
	return Run(ctx, cfg, req)
}

func IsTerminalEvent(kind EventKind) bool {
	return kind == EventCompleted || kind == EventFailed || kind == EventCanceled
}

func Run(ctx context.Context, cfg Config, req tools.AgentRequest) (tools.AgentResult, error) {
	return run(ctx, cfg, req, false)
}

func run(ctx context.Context, cfg Config, req tools.AgentRequest, taskLeaseHeld bool) (result tools.AgentResult, runErr error) {
	if cfg.Client == nil {
		return tools.AgentResult{}, errors.New("subagent client unavailable")
	}
	if strings.TrimSpace(cfg.Root) == "" {
		return tools.AgentResult{}, errors.New("subagent project root is empty")
	}
	if req.Fork && cfg.ParentIsFork {
		return tools.AgentResult{}, errors.New("a conversation fork cannot create another fork")
	}
	if req.Fork && len(cfg.ParentMessages) == 0 {
		return tools.AgentResult{}, errors.New("conversation fork requires a parent context snapshot")
	}
	storeProject := strings.TrimSpace(cfg.StoreProject)
	if storeProject == "" {
		storeProject = cfg.Root
	}
	originalRoot := cfg.Root
	var provider providers.Provider
	var model string
	var err error

	settings, _ := config.Load(cfg.ConfigDir)
	projectTrusted := config.IsProjectTrusted(settings, originalRoot)
	hookRunner := &hooks.Runner{}
	if settings.HooksEnabled {
		hookRunner = hooks.Load(cfg.ConfigDir, cfg.Root, projectTrusted)
		hookRunner.Merge(cfg.PluginHooks)
		if strings.TrimSpace(req.Agent.HooksRaw) != "" && (req.Agent.Source != "project" || projectTrusted) {
			hookRunner.Merge(hooks.ParseFrontmatter(req.Agent.HooksRaw, req.Agent.BaseDir))
		}
	}
	memoryDir := ""
	if settings.AutoMemoryEnabled {
		switch strings.ToLower(strings.TrimSpace(req.Agent.Memory)) {
		case "user":
			memoryDir = limemory.AgentDir(cfg.ConfigDir, cfg.Root, req.Agent.Name, limemory.User)
		case "project":
			memoryDir = limemory.AgentDir(cfg.ConfigDir, cfg.Root, req.Agent.Name, limemory.Project)
		case "local":
			memoryDir = limemory.AgentDir(cfg.ConfigDir, cfg.Root, req.Agent.Name, limemory.Local)
		}
	}

	depth := cfg.Depth
	if depth <= 0 {
		depth = 1
	}
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = configuredMaxDepth()
	}

	store := newChildStore(cfg.ConfigDir, storeProject)
	resumed := false
	var child *childSession
	if strings.TrimSpace(req.TaskID) != "" {
		if !taskLeaseHeld {
			releaseTask, acquireErr := store.acquire(req.TaskID)
			if acquireErr != nil {
				return tools.AgentResult{}, acquireErr
			}
			defer releaseTask()
		}
		child, err = store.load(req.TaskID)
		if err != nil {
			return tools.AgentResult{}, fmt.Errorf("resume subagent %s: %w", req.TaskID, err)
		}
		if !strings.EqualFold(child.AgentName, req.Agent.Name) {
			return tools.AgentResult{}, fmt.Errorf("task %s belongs to agent %s, not %s", child.ID, child.AgentName, req.Agent.Name)
		}
		if filepathClean(child.Project) != filepathClean(storeProject) {
			return tools.AgentResult{}, fmt.Errorf("task %s belongs to a different project", child.ID)
		}
		if strings.TrimSpace(child.WorktreeRoot) != "" {
			info, statErr := os.Stat(child.WorktreeRoot)
			if statErr != nil || !info.IsDir() {
				if statErr == nil {
					statErr = errors.New("path is not a directory")
				}
				return tools.AgentResult{}, fmt.Errorf("task %s worktree is unavailable: %s: %w", child.ID, child.WorktreeRoot, statErr)
			}
			cfg.Root = child.WorktreeRoot
		}
		providerFromStore := cfg.Providers.FindProvider(strings.TrimSpace(child.ProviderID))
		if providerFromStore == nil {
			return tools.AgentResult{}, fmt.Errorf("task %s provider %q is no longer configured", child.ID, child.ProviderID)
		}
		model = strings.TrimSpace(child.ModelID)
		if model == "" {
			return tools.AgentResult{}, fmt.Errorf("task %s has no persisted model", child.ID)
		}
		provider = *providerFromStore
		resumed = true
		child.ParentTaskID = cfg.ParentTaskID
		child.Description = req.Description
		child.Depth = depth
		child.Status = "running"
		child.FinishedAt = time.Time{}
		ci := codeIntelForConfig(cfg)
		child.Messages = mergeCodeIntelSystemMessage(child.Messages, ci.PromptBlock())
		child.Messages = append(child.Messages, openai.Message{Role: "user", Content: req.Prompt})
	} else {
		provider, model, err = resolveModel(cfg.Providers, cfg.ParentProviderID, cfg.ParentModelID, req.Agent.Model, req.Model)
		if err != nil {
			return tools.AgentResult{}, err
		}
		now := timeNow()
		taskID := strings.TrimSpace(req.AllocatedTaskID)
		if taskID == "" {
			taskID = newTaskID()
		}
		if !taskLeaseHeld {
			releaseTask, acquireErr := store.acquire(taskID)
			if acquireErr != nil {
				return tools.AgentResult{}, acquireErr
			}
			defer releaseTask()
		}
		worktreeRoot := ""
		worktreeCustom := false
		isolation := strings.TrimSpace(req.Isolation)
		if isolation == "" {
			isolation = strings.TrimSpace(req.Agent.Isolation)
		}
		if strings.EqualFold(isolation, "worktree") {
			wt, custom, wtErr := createWorktree(ctx, cfg.ConfigDir, originalRoot, taskID, projectTrusted, hookRunner)
			if wtErr != nil {
				return tools.AgentResult{}, fmt.Errorf("agent %s worktree: %w", req.Agent.Name, wtErr)
			}
			worktreeRoot = wt
			worktreeCustom = custom
			cfg.Root = wt
		}
		var msgs []openai.Message
		if req.Fork {
			msgs = SanitizeForkMessages(cfg.ParentMessages)
			if strings.TrimSpace(req.Agent.Prompt) != "" && !strings.EqualFold(req.Agent.Name, "fork") {
				agentPrompt := strings.TrimSpace(req.Agent.Prompt)
				if strings.TrimSpace(req.Agent.PluginRoot) != "" {
					agentPrompt = strings.ReplaceAll(agentPrompt, "${CLAUDE_PLUGIN_ROOT}", filepathSlash(req.Agent.PluginRoot))
				}
				msgs = append(msgs, openai.Message{Role: "user", Content: "<fork_agent_instructions agent=\"" + req.Agent.Name + "\">\n" + agentPrompt + "\n</fork_agent_instructions>"})
			}
			if worktreeRoot != "" {
				msgs = append(msgs, openai.Message{Role: "user", Content: "<fork_environment>\nWorking directory: " + filepathSlash(cfg.Root) + "\nThis fork is isolated in a git worktree.\n</fork_environment>"})
			}
			ci := codeIntelForConfig(cfg)
			msgs = mergeCodeIntelSystemMessage(msgs, ci.PromptBlock())
		} else {
			ci := codeIntelForConfig(cfg)
			msgs = []openai.Message{{Role: "system", Content: buildSystemPrompt(req.Agent, cfg.Root, cfg.Skills, cfg.Agents, depth < maxDepth, memoryDir, ci.PromptBlock())}}
			// Claude's built-in Explore/Plan intentionally omit CLAUDE.md. Custom
			// subagents receive the normal project instruction flow before the task.
			if !strings.EqualFold(req.Agent.Name, "Explore") && !strings.EqualFold(req.Agent.Name, "Plan") {
				bundle := instructions.Load(instructions.Options{ConfigDir: cfg.ConfigDir, CWD: cfg.Root, NativeEnabled: settings.ProjectInstructionsEnabled, ClaudeEnabled: settings.ClaudeCompatibilityEnabled, ProjectTrusted: projectTrusted})
				if block := strings.TrimSpace(bundle.StaticPrompt()); block != "" {
					msgs = append(msgs, openai.Message{Role: "user", Content: block})
				}
			}
		}
		msgs = append(msgs, openai.Message{Role: "user", Content: req.Prompt})
		child = &childSession{
			ID: taskID, AgentName: req.Agent.Name, ParentTaskID: cfg.ParentTaskID, Description: req.Description, Depth: depth, Status: "running", Project: filepathClean(storeProject), WorktreeRoot: worktreeRoot, WorktreeCustom: worktreeCustom,
			ProviderID: provider.ID, ModelID: model, CreatedAt: now, UpdatedAt: now, Messages: msgs,
		}
	}

	// WorktreeCreate must run from the original checkout, but every subsequent
	// subagent hook/tool should observe the isolated cwd.
	if hookRunner != nil {
		hookRunner.Root = cfg.Root
	}

	codeIntel := codeIntelForConfig(cfg)

	localTodos := litodo.NewManager(nil)
	mode := planstate.Build
	if cfg.ParentMode == planstate.Plan {
		mode = planstate.Plan
	} else if strings.EqualFold(strings.TrimSpace(req.Agent.PermissionMode), "plan") {
		mode = planstate.Plan
	}

	policy := newToolPolicy(req.Agent, mode, req.Background)

	// Claude subagents inherit the parent session's MCP tools. Inline mcpServers
	// are additive and get a child-owned runtime; project-provided executable MCP
	// configuration is honored only for trusted projects.
	var inlineMCP *mcp.Runtime
	if settings.ClaudeCompatibilityEnabled && strings.TrimSpace(req.Agent.MCPRaw) != "" {
		if req.Agent.Source != "project" || projectTrusted {
			inlineConfigs := mcp.ParseInlineServers(req.Agent.MCPRaw)
			if len(inlineConfigs) > 0 {
				inlineMCP = mcp.NewRuntime()
				mcpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				errs := inlineMCP.Connect(mcpCtx, inlineConfigs)
				cancel()
				if len(errs) > 0 {
					_ = inlineMCP.Close()
					return tools.AgentResult{}, fmt.Errorf("agent %s MCP: %v", req.Agent.Name, errs)
				}
				defer inlineMCP.Close()
			}
		}
	}
	if hookRunner.Count() > 0 {
		hookRunner.MCPTool = func(toolCtx context.Context, server, tool string, input map[string]any) (string, error) {
			var parentErr error
			if cfg.ParentMCP != nil {
				if text, callErr := cfg.ParentMCP.CallServerTool(toolCtx, server, tool, input); callErr == nil {
					return text, nil
				} else {
					parentErr = callErr
				}
			}
			if inlineMCP != nil {
				if text, callErr := inlineMCP.CallServerTool(toolCtx, server, tool, input); callErr == nil {
					return text, nil
				} else if parentErr == nil {
					return "", callErr
				}
			}
			if parentErr != nil {
				return "", parentErr
			}
			return "", fmt.Errorf("MCP tool unavailable: %s/%s", server, tool)
		}
	}

	var active []string
	env := tools.Env{Root: cfg.Root, CodeIntel: codeIntel, ConfigDir: cfg.ConfigDir, Skills: cfg.Skills, Todos: localTodos, AgentMode: mode, MemoryDir: memoryDir}
	env.DynamicTool = func(toolCtx context.Context, name string, args map[string]any) (string, error) {
		if cfg.ParentMCP != nil && cfg.ParentMCP.Has(name) {
			if !policy.allowsDynamic(name, cfg.ParentMCP.IsReadOnly(name)) {
				return "", fmt.Errorf("MCP tool %s is not allowed for this subagent", name)
			}
			return cfg.ParentMCP.Call(toolCtx, name, args)
		}
		if inlineMCP != nil && inlineMCP.Has(name) {
			if !policy.allowsDynamic(name, inlineMCP.IsReadOnly(name)) {
				return "", fmt.Errorf("MCP tool %s is not allowed for this subagent", name)
			}
			return inlineMCP.Call(toolCtx, name, args)
		}
		return "", fmt.Errorf("MCP tool unavailable: %s", name)
	}
	if hookRunner.Count() > 0 {
		env.BeforeTool = func(toolCtx context.Context, name string, args map[string]any) (map[string]any, error) {
			cn := claudeToolName(name)
			input := map[string]any{"session_id": child.ID, "cwd": cfg.Root, "hook_event_name": "PreToolUse", "tool_name": cn, "tool_input": args}
			res, e := hookRunner.Run(toolCtx, "PreToolUse", cn, input)
			if e != nil {
				return nil, e
			}
			if res.Blocked {
				return nil, fmt.Errorf("tool blocked by hook: %s", res.Reason)
			}
			if res.UpdatedInput != nil {
				return res.UpdatedInput, nil
			}
			return args, nil
		}
		env.AfterTool = func(toolCtx context.Context, name string, args map[string]any, output string, runErr error) (string, error) {
			event := "PostToolUse"
			input := map[string]any{"session_id": child.ID, "cwd": cfg.Root, "tool_name": claudeToolName(name), "tool_input": args}
			if runErr != nil {
				event = "PostToolUseFailure"
				input["error"] = runErr.Error()
			} else {
				input["tool_response"] = output
			}
			input["hook_event_name"] = event
			res, e := hookRunner.Run(toolCtx, event, claudeToolName(name), input)
			if e != nil {
				return output, e
			}
			if res.Blocked {
				return output, fmt.Errorf("tool result blocked by hook: %s", res.Reason)
			}
			if res.UpdatedOutput != "" {
				output = res.UpdatedOutput
			}
			if res.AdditionalContext != "" {
				output += "\n\n<hook_context>\n" + res.AdditionalContext + "\n</hook_context>"
			}
			return output, nil
		}
	}
	if depth < maxDepth && len(cfg.Agents) > 0 {
		env.Agents = cfg.Agents
		env.RunAgent = func(childCtx context.Context, childReq tools.AgentRequest) (tools.AgentResult, error) {
			if req.Fork && childReq.Fork {
				return tools.AgentResult{}, errors.New("a conversation fork cannot create another fork")
			}
			nested := cfg
			nested.ParentProviderID = provider.ID
			nested.ParentModelID = model
			nested.ParentMode = mode
			nested.ParentMessages = SanitizeForkMessages(child.Messages)
			nested.ParentToolNames = append([]string(nil), active...)
			nested.ParentIsFork = req.Fork
			nested.Depth = depth + 1
			nested.MaxDepth = maxDepth
			nested.ParentTaskID = child.ID
			return Dispatch(childCtx, nested, childReq, backgroundTasksEnabled())
		}
	}
	env.ToolVisible = func(name string, def tools.Definition) bool { return policy.visible(name, def) }
	env.ValidateTool = func(name string, def tools.Definition, args map[string]any) error {
		if err := policy.validate(name, def, args); err != nil {
			return err
		}
		if child.WorktreeRoot != "" && name == "run_terminal_command" {
			return validateWorktreeCommand(toolCommand(args), originalRoot)
		}
		return nil
	}

	active = append([]string(nil), child.Tools...)
	if len(active) == 0 && req.Fork {
		active = append([]string(nil), cfg.ParentToolNames...)
	}
	if len(active) == 0 {
		active = policy.initialTools(req.Prompt, env)
	}
	if memoryDir != "" {
		active = appendUnique(active, "memory_read")
		if mode != planstate.Plan {
			active = appendUnique(active, "memory_write")
		}
	}
	active = tools.FilterAvailable(active, env)
	child.Tools = append([]string(nil), active...)
	if err := store.save(child); err != nil {
		return tools.AgentResult{}, err
	}
	if hookRunner.Count() > 0 {
		input := map[string]any{"session_id": child.ID, "cwd": cfg.Root, "hook_event_name": "SubagentStart", "agent_type": req.Agent.Name}
		res, hookErr := hookRunner.Run(ctx, "SubagentStart", req.Agent.Name, input)
		if hookErr == nil && res.Blocked {
			hookErr = fmt.Errorf("subagent blocked by hook: %s", res.Reason)
		}
		if hookErr != nil {
			child.Status = "failed"
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: EventFailed, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Resumed: resumed, Background: req.Background, Content: hookErr.Error(), At: child.FinishedAt})
			return tools.AgentResult{}, hookErr
		}
	}
	// From EventStarted onward every exit path must publish one terminal event.
	// Most branches emit an exact lifecycle state themselves; this guard covers
	// infrastructure failures such as a persistence error after a tool round.
	originalEvents := cfg.Events
	var lifecycleMu sync.Mutex
	terminalSeen := false
	cfg.Events = func(event Event) {
		if event.TaskID == child.ID && IsTerminalEvent(event.Kind) {
			lifecycleMu.Lock()
			terminalSeen = true
			lifecycleMu.Unlock()
		}
		if originalEvents != nil {
			originalEvents(event)
		}
	}
	emit(cfg, Event{
		Kind: EventStarted, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID,
		AgentName: req.Agent.Name, Description: req.Description, Model: model,
		Depth: depth, Resumed: resumed, Background: req.Background, At: timeNow(),
	})
	defer func() {
		if hookRunner.Count() == 0 {
			return
		}
		input := map[string]any{"session_id": child.ID, "cwd": cfg.Root, "hook_event_name": "SubagentStop", "agent_type": req.Agent.Name, "status": child.Status}
		_, _ = hookRunner.Run(context.Background(), "SubagentStop", req.Agent.Name, input)
	}()
	defer func() {
		if runErr == nil {
			return
		}
		lifecycleMu.Lock()
		alreadyTerminal := terminalSeen
		if !alreadyTerminal {
			terminalSeen = true
		}
		lifecycleMu.Unlock()
		if alreadyTerminal {
			return
		}
		kind := EventFailed
		child.Status = "failed"
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = EventCanceled
			child.Status = "killed"
		}
		child.FinishedAt = timeNow()
		_ = store.save(child)
		if originalEvents != nil {
			originalEvents(Event{
				Kind: kind, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID,
				AgentName: req.Agent.Name, Description: req.Description, Model: model,
				Depth: depth, Resumed: resumed, Background: req.Background,
				Content: runErr.Error(), At: child.FinishedAt,
			})
		}
	}()

	turns := 0
	for {
		if err := ctx.Err(); err != nil {
			child.Status = "killed"
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: EventCanceled, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: err.Error(), At: timeNow()})
			return tools.AgentResult{}, err
		}
		turns++
		if req.Agent.MaxTurns > 0 && turns > req.Agent.MaxTurns {
			err := fmt.Errorf("subagent %s reached its configured maxTurns=%d", req.Agent.Name, req.Agent.MaxTurns)
			child.Status = "failed"
			child.FinishedAt = timeNow()
			_ = store.save(child)
			emit(cfg, Event{Kind: EventFailed, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: err.Error(), At: timeNow()})
			return tools.AgentResult{}, err
		}

		var schemas []any
		for _, schema := range tools.Schemas(active) {
			schemas = append(schemas, schema)
		}
		if cfg.ParentMCP != nil {
			schemas = append(schemas, filterMCPSchemas(cfg.ParentMCP.SchemasForMode(mode == planstate.Plan), cfg.ParentMCP, policy)...)
		}
		if inlineMCP != nil {
			schemas = append(schemas, filterMCPSchemas(inlineMCP.SchemasForMode(mode == planstate.Plan), inlineMCP, policy)...)
		}
		ch := cfg.Client.Stream(ctx, openai.Request{Provider: provider, Model: model, Messages: child.Messages, Stream: true, Tools: schemas, ReasoningEffort: req.Agent.Effort})
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
			emit(cfg, Event{Kind: kind, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: wrapped.Error(), At: timeNow()})
			return tools.AgentResult{}, wrapped
		}
		assistant := openai.Message{Role: "assistant", Content: text, ReasoningContent: reasoning, ToolCalls: calls}
		child.Messages = append(child.Messages, assistant)
		if len(calls) == 0 {
			child.Status = "completed"
			child.FinishedAt = timeNow()
			if child.WorktreeRoot != "" {
				if hookRunner != nil {
					hookRunner.Root = originalRoot
				}
				if removed, cleanErr := cleanupWorktree(context.Background(), originalRoot, child.WorktreeRoot, child.WorktreeCustom, hookRunner); cleanErr == nil && removed {
					child.WorktreeRoot = ""
				} else if !removed {
					text = strings.TrimSpace(text) + "\n\nWorktree con cambios conservado en: " + child.WorktreeRoot
				}
			}
			if err := store.save(child); err != nil {
				return tools.AgentResult{}, err
			}
			emit(cfg, Event{Kind: EventCompleted, TaskID: child.ID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: strings.TrimSpace(text), At: timeNow()})
			return tools.AgentResult{TaskID: child.ID, AgentName: req.Agent.Name, Text: strings.TrimSpace(text), Resumed: resumed, Background: req.Background}, nil
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
		if chunk.Retry != nil {
			if chunk.Retry.Reset {
				tb.Reset()
				rb.Reset()
				calls = nil
				emit(cfg, Event{Kind: EventStreamReset, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, At: timeNow()})
			}
			continue
		}
		if chunk.Delta != "" {
			tb.WriteString(chunk.Delta)
			emit(cfg, Event{Kind: EventText, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: chunk.Delta, At: timeNow()})
		}
		if chunk.Thinking != "" {
			rb.WriteString(chunk.Thinking)
			emit(cfg, Event{Kind: EventThinking, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, Content: chunk.Thinking, At: timeNow()})
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
	emit(cfg, Event{Kind: kind, TaskID: taskID, ParentTaskID: cfg.ParentTaskID, AgentName: req.Agent.Name, Description: req.Description, Model: model, Depth: depth, Background: req.Background, ToolCallID: call.ID, ToolName: call.Function.Name, ToolArgs: call.Function.Arguments, Content: content, At: timeNow()})
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

func mergeCodeIntelSystemMessage(messages []openai.Message, profile string) []openai.Message {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return messages
	}
	block := "<code_intelligence>\n" + profile + "\n</code_intelligence>"
	out := append([]openai.Message(nil), messages...)
	for i := range out {
		if out[i].Role != "system" {
			continue
		}
		if strings.Contains(out[i].Content, "<code_intelligence>") {
			return out
		}
		out[i].Content = strings.TrimSpace(out[i].Content) + "\n\n" + block
		return out
	}
	return append([]openai.Message{{Role: "system", Content: block}}, out...)
}

func buildSystemPrompt(a agents.Agent, root string, catalog []skills.Skill, agentCatalog []agents.Agent, canDelegate bool, memoryDir, codeIntelProfile string) string {
	var b strings.Builder
	prompt := strings.TrimSpace(a.Prompt)
	if strings.TrimSpace(a.PluginRoot) != "" {
		prompt = strings.ReplaceAll(prompt, "${CLAUDE_PLUGIN_ROOT}", filepathSlash(a.PluginRoot))
	}
	if prompt == "" {
		prompt = "Complete the delegated task accurately and return a concise final result."
	}
	b.WriteString(prompt)
	// Claude's initialPrompt applies when a definition is used as the primary
	// session agent. Spawned subagents receive only their system prompt + task.
	if memoryDir != "" {
		b.WriteString(limemory.Prompt(memoryDir))
	}
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
	if strings.TrimSpace(codeIntelProfile) != "" {
		b.WriteString("\n\n<code_intelligence>\n")
		b.WriteString(strings.TrimSpace(codeIntelProfile))
		b.WriteString("\n</code_intelligence>")
	}
	return b.String()
}

func codeIntelForConfig(cfg Config) *codeintel.Manager {
	if cfg.CodeIntel != nil && filepathClean(cfg.CodeIntel.Root()) == filepathClean(cfg.Root) {
		return cfg.CodeIntel
	}
	return codeintel.New(cfg.Root, cfg.ConfigDir)
}

func preloadSkills(names []string, catalog []skills.Skill) string {
	if len(names) == 0 || len(catalog) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range names {
		sk := skills.Find(catalog, name)
		if sk == nil || sk.DisableModelInvocation {
			continue
		}
		body, err := skills.ReadContent(*sk)
		if err != nil {
			continue
		}
		body = skills.ExpandArguments(*sk, body, "")
		fmt.Fprintf(&b, "\n\n<preloaded_skill name=\"%s\">\n%s\n</preloaded_skill>", sk.Name, strings.TrimSpace(body))
	}
	return b.String()
}

type toolPolicy struct {
	mode       planstate.Mode
	background bool
	allow      map[string]bool // nil means inherit all
	deny       map[string]bool
}

func newToolPolicy(a agents.Agent, mode planstate.Mode, background bool) toolPolicy {
	p := toolPolicy{mode: mode, background: background, deny: map[string]bool{"plan_question": true, "plan_exit": true}}
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
	if p.background && !backgroundBuiltinAllowed(name) {
		return false
	}
	if p.deny[name] {
		return false
	}
	if p.allow != nil && !p.allow[name] {
		return false
	}
	return planstate.ToolVisible(p.mode, name, def.Mutating)
}

func backgroundBuiltinAllowed(name string) bool {
	switch name {
	case "Agent", "read_files", "list_directory", "glob", "code_search", "code_intel_status",
		"code_symbols", "code_references", "code_graph", "code_context",
		"code_semantic", "code_scip_search", "run_terminal_command",
		"str_replace", "apply_diff", "create_file", "write_file", "append_file", "read_url", "web_search", "todo_write",
		"list_skills", "skill_read", "skill_search", "skill_files", "tool_search",
		"memory_read", "memory_write":
		return true
	default:
		return false
	}
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
func (p toolPolicy) allowsDynamic(name string, readOnly bool) bool {
	if p.deny[name] {
		return false
	}
	if p.allow != nil && !p.allow[name] {
		return false
	}
	if p.mode == planstate.Plan && !readOnly {
		return false
	}
	return true
}
func filterMCPSchemas(schemas []any, runtime *mcp.Runtime, policy toolPolicy) []any {
	out := make([]any, 0, len(schemas))
	for _, schema := range schemas {
		m, ok := schema.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := m["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" || runtime == nil || !runtime.Has(name) {
			continue
		}
		if policy.allowsDynamic(name, runtime.IsReadOnly(name)) {
			out = append(out, schema)
		}
	}
	return out
}

func toolCommand(args map[string]any) string {
	for _, key := range []string{"command", "cmd", "script"} {
		if value, ok := args[key].(string); ok {
			return value
		}
	}
	return ""
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
	if strings.HasPrefix(strings.ToLower(v), "mcp__") {
		return []string{v}
	}
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
		return []string{"create_file", "write_file", "append_file", "str_replace", "apply_diff"}
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
		return []string{"create_file", "write_file", "append_file", "str_replace", "apply_diff"}
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

func backgroundTasksEnabled() bool {
	return strings.TrimSpace(os.Getenv("CLAUDE_CODE_DISABLE_BACKGROUND_TASKS")) != "1"
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
	want := strings.TrimSpace(os.Getenv("CLAUDE_CODE_SUBAGENT_MODEL"))
	if strings.EqualFold(want, "inherit") {
		// Preserve the existing Claude-compatible behavior: an environment value
		// of inherit disables the global override so the invocation/agent may
		// still choose its own model.
		want = ""
	}
	if want == "" {
		want = strings.TrimSpace(invocationModel)
	}
	if want == "" {
		want = strings.TrimSpace(agentModel)
	}

	candidates := splitModelCandidates(want)
	if len(candidates) == 0 {
		return parentModelSelection(cfg, parentProvider, parentModel)
	}

	hasClaudeAlias := false
	for _, candidate := range candidates {
		if isDefaultModel(candidate) {
			return parentModelSelection(cfg, parentProvider, parentModel)
		}
		if isClaudeModelAlias(candidate) {
			hasClaudeAlias = true
		}
		if provider, model, ok := resolveModelCandidate(cfg, parentProvider, candidate); ok {
			return provider, model, nil
		}
	}

	// Portable Claude agents often pin sonnet/opus/haiku. On a Lilith setup
	// backed only by DeepSeek/Qwen/etc. there is no equivalent alias; inheriting
	// the parent is more useful than making an otherwise portable agent fail.
	// For a comma-separated list we try every explicit candidate first and only
	// then apply this compatibility fallback.
	if hasClaudeAlias {
		return parentModelSelection(cfg, parentProvider, parentModel)
	}
	return providers.Provider{}, "", fmt.Errorf("subagent models %q are not configured; use default/inherit, provider/model, or a comma-separated preference list", want)
}

func splitModelCandidates(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isDefaultModel(value string) bool {
	value = strings.TrimSpace(value)
	return strings.EqualFold(value, "default") || strings.EqualFold(value, "inherit")
}

func isClaudeModelAlias(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sonnet", "opus", "haiku", "fable":
		return true
	default:
		return false
	}
}

func parentModelSelection(cfg providers.Config, parentProvider, parentModel string) (providers.Provider, string, error) {
	p := cfg.FindProvider(parentProvider)
	if p == nil {
		return providers.Provider{}, "", errors.New("parent provider not found")
	}
	if strings.TrimSpace(parentModel) == "" {
		return providers.Provider{}, "", errors.New("parent model not found")
	}
	return *p, parentModel, nil
}

func resolveModelCandidate(cfg providers.Config, parentProvider, candidate string) (providers.Provider, string, bool) {
	if strings.Contains(candidate, "/") {
		parts := strings.SplitN(candidate, "/", 2)
		if p := cfg.FindProvider(parts[0]); p != nil && providerHasModel(*p, parts[1]) {
			return *p, parts[1], true
		}
	}

	// Claude aliases and exact model names are resolved pragmatically against
	// configured model IDs, preferring the provider already selected by /models.
	needle := strings.ToLower(strings.TrimSpace(candidate))
	if p := cfg.FindProvider(parentProvider); p != nil {
		if model := findModel(*p, needle); model != "" {
			return *p, model, true
		}
	}
	for _, p := range cfg.Providers {
		if model := findModel(p, needle); model != "" {
			return p, model, true
		}
	}
	return providers.Provider{}, "", false
}
func findModel(p providers.Provider, want string) string {
	for _, m := range p.Models {
		if strings.EqualFold(m.ID, want) || strings.EqualFold(m.Name, want) {
			return m.ID
		}
	}
	if want == "sonnet" || want == "opus" || want == "haiku" || want == "fable" {
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

func claudeToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "run_terminal_command":
		return "Bash"
	case "read_files":
		return "Read"
	case "glob", "list_directory":
		return "Glob"
	case "code_search":
		return "Grep"
	case "create_file", "write_file", "append_file":
		return "Write"
	case "str_replace", "apply_diff":
		return "Edit"
	case "read_url":
		return "WebFetch"
	case "web_search":
		return "WebSearch"
	case "agent", "task":
		return "Agent"
	default:
		return name
	}
}
