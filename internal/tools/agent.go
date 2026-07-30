package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lilith/li/internal/agents"
)

// AgentRequest is intentionally provider-agnostic. The chat runtime resolves
// model inheritance, child-session persistence and the actual tool loop.
type AgentRequest struct {
	Agent       agents.Agent
	Prompt      string
	Description string
	TaskID      string
	Model       string
	Background  bool
	// Fork requests a Claude-compatible conversation fork instead of a fresh
	// isolated worker. The host supplies the inherited model-facing messages.
	Fork      bool
	Isolation string
	// AllocatedTaskID lets a host reserve an id before detaching the worker.
	AllocatedTaskID string
}

// AgentResult is the final isolated handoff returned to the parent model.
type AgentResult struct {
	TaskID     string
	AgentName  string
	Text       string
	Resumed    bool
	Background bool
}

func init() {
	register(Definition{
		Name: "Agent",
		Description: "Delegate work to a specialized isolated subagent, or use subagent_type=\"fork\" to branch the current conversation with its full model-facing context. " +
			"Named subagents receive only the delegated task and their own prompt/tool policy. Forks inherit conversation history, instructions, model and active tools. " +
			"Use task_id only to continue a previous child session. Foreground calls return the final result; background calls return a task id immediately and deliver completion later.",
		PromptSnippet: "Delegate isolated work to a specialized subagent",
		PromptGuidelines: []string{
			"Act as an orchestrator when useful: delegate independent, self-contained investigations or implementation units instead of doing all work in the parent context.",
			"When two or more delegated tasks are independent, emit multiple Agent calls in the same assistant response so Lilith can execute them concurrently; wait for their results, then synthesize or continue.",
			"A named subagent may delegate further while below the configured nesting depth. Give isolated children enough context to work without the parent conversation.",
			"Use subagent_type=\"fork\" only when the child genuinely needs the current conversation history. A fork cannot create another fork.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":       map[string]any{"type": "string", "description": "Short 3-5 word description of the delegated task."},
				"prompt":            map[string]any{"type": "string", "description": "Complete task and all context the isolated subagent needs."},
				"subagent_type":     map[string]any{"type": "string", "description": "Name from <available_agents>, or fork to inherit the current conversation."},
				"task_id":           map[string]any{"type": "string", "description": "Optional prior task id to resume instead of creating a fresh child session."},
				"model":             map[string]any{"type": "string", "description": "Optional per-invocation model override. Use default/inherit for the model selected in /models, one configured model, or a comma-separated preference list."},
				"run_in_background": map[string]any{"type": "boolean", "description": "Run concurrently and return a task id immediately instead of blocking for the final result."},
				"isolation":         map[string]any{"type": "string", "enum": []string{"worktree"}, "description": "Optional git worktree isolation for this invocation."},
			},
			"required": []string{"description", "prompt", "subagent_type"},
		},
		Available: func(env Env) bool { return env.RunAgent != nil },
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			name := strings.TrimSpace(str(args, "subagent_type"))
			prompt := strings.TrimSpace(str(args, "prompt"))
			description := strings.TrimSpace(str(args, "description"))
			if name == "" || prompt == "" || description == "" {
				return "", fmt.Errorf("Agent requires description, prompt and subagent_type")
			}
			fork := strings.EqualFold(name, "fork")
			if fork && strings.TrimSpace(os.Getenv("CLAUDE_CODE_FORK_SUBAGENT")) == "0" {
				return "", fmt.Errorf("fork subagents are disabled by CLAUDE_CODE_FORK_SUBAGENT=0")
			}
			a := agents.Find(env.Agents, name)
			if fork {
				a = &agents.Agent{Name: "fork", Description: "Conversation fork", Model: "default"}
			}
			if a == nil {
				available := make([]string, 0, len(env.Agents)+1)
				for _, candidate := range env.Agents {
					available = append(available, candidate.Name)
				}
				available = append(available, "fork")
				return "", fmt.Errorf("unknown subagent_type %q; available: %s", name, strings.Join(available, ", "))
			}
			background, hasBackground := boolValue(args, "run_in_background")
			if !hasBackground && a.BackgroundSet {
				background = a.Background
			}
			result, err := env.RunAgent(ctx, AgentRequest{
				Agent: *a, Prompt: prompt, Description: description,
				TaskID: strings.TrimSpace(str(args, "task_id")), Model: strings.TrimSpace(str(args, "model")), Background: background,
				Fork: fork, Isolation: strings.TrimSpace(str(args, "isolation")),
			})
			if err != nil {
				return "", err
			}
			state := "completed"
			if result.Background {
				state = "running"
			}
			resume := ""
			if result.Resumed {
				resume = " resumed=\"true\""
			}
			return fmt.Sprintf("<agent_result id=\"%s\" agent=\"%s\" state=\"%s\"%s>\n%s\n</agent_result>", result.TaskID, result.AgentName, state, resume, strings.TrimSpace(result.Text)), nil
		},
	})
}

func boolValue(args map[string]any, key string) (bool, bool) {
	v, ok := args[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}
