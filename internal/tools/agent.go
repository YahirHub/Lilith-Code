package tools

import (
	"context"
	"fmt"
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
		Description: "Delegate a self-contained task to a specialized subagent running in an isolated context. " +
			"Choose subagent_type from <available_agents>. The subagent receives the task and its own system prompt/tool policy, not the parent's full conversation. " +
			"Use task_id only to continue a previous subagent session. Foreground calls return the final result; background calls return a task id immediately and deliver completion later.",
		PromptSnippet: "Delegate isolated work to a specialized subagent",
		PromptGuidelines: []string{
			"Act as an orchestrator when useful: delegate independent, self-contained investigations or implementation units instead of doing all work in the parent context.",
			"When two or more delegated tasks are independent, emit multiple Agent calls in the same assistant response so Lilith can execute them concurrently; wait for their results, then synthesize or continue.",
			"A subagent may delegate further while below the configured nesting depth. Give every child enough context to work without the parent conversation.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":       map[string]any{"type": "string", "description": "Short 3-5 word description of the delegated task."},
				"prompt":            map[string]any{"type": "string", "description": "Complete task and all context the isolated subagent needs."},
				"subagent_type":     map[string]any{"type": "string", "description": "Name from <available_agents>."},
				"task_id":           map[string]any{"type": "string", "description": "Optional prior task id to resume instead of creating a fresh child session."},
				"model":             map[string]any{"type": "string", "description": "Optional per-invocation model override. Use default/inherit for the model selected in /models, one configured model, or a comma-separated preference list."},
				"run_in_background": map[string]any{"type": "boolean", "description": "Run concurrently and return a task id immediately instead of blocking for the final result."},
			},
			"required": []string{"description", "prompt", "subagent_type"},
		},
		Available: func(env Env) bool { return len(env.Agents) > 0 && env.RunAgent != nil },
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			name := strings.TrimSpace(str(args, "subagent_type"))
			prompt := strings.TrimSpace(str(args, "prompt"))
			description := strings.TrimSpace(str(args, "description"))
			if name == "" || prompt == "" || description == "" {
				return "", fmt.Errorf("Agent requires description, prompt and subagent_type")
			}
			a := agents.Find(env.Agents, name)
			if a == nil {
				available := make([]string, 0, len(env.Agents))
				for _, candidate := range env.Agents {
					available = append(available, candidate.Name)
				}
				return "", fmt.Errorf("unknown subagent_type %q; available: %s", name, strings.Join(available, ", "))
			}
			background, hasBackground := boolValue(args, "run_in_background")
			if !hasBackground && a.BackgroundSet {
				background = a.Background
			}
			result, err := env.RunAgent(ctx, AgentRequest{
				Agent: *a, Prompt: prompt, Description: description,
				TaskID: strings.TrimSpace(str(args, "task_id")), Model: strings.TrimSpace(str(args, "model")), Background: background,
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
