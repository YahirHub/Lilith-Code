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
}

// AgentResult is the final isolated handoff returned to the parent model.
type AgentResult struct {
	TaskID    string
	AgentName string
	Text      string
	Resumed   bool
}

func init() {
	register(Definition{
		Name: "Agent",
		Description: "Delegate a self-contained task to a specialized subagent running in an isolated context. " +
			"Choose subagent_type from <available_agents>. The subagent receives the task and its own system prompt/tool policy, not the parent's full conversation. " +
			"Use task_id only to continue a previous subagent session. The final result is returned to this conversation.",
		PromptSnippet: "Delegate isolated work to a specialized subagent",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string", "description": "Short 3-5 word description of the delegated task."},
				"prompt":        map[string]any{"type": "string", "description": "Complete task and all context the isolated subagent needs."},
				"subagent_type": map[string]any{"type": "string", "description": "Name from <available_agents>."},
				"task_id":       map[string]any{"type": "string", "description": "Optional prior task id to resume instead of creating a fresh child session."},
				"model":         map[string]any{"type": "string", "description": "Optional per-invocation model override. Prefer inherit unless a specific configured model is required."},
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
			result, err := env.RunAgent(ctx, AgentRequest{
				Agent: *a, Prompt: prompt, Description: description,
				TaskID: strings.TrimSpace(str(args, "task_id")), Model: strings.TrimSpace(str(args, "model")),
			})
			if err != nil {
				return "", err
			}
			state := "completed"
			resume := ""
			if result.Resumed {
				resume = " resumed=\"true\""
			}
			return fmt.Sprintf("<agent_result id=\"%s\" agent=\"%s\" state=\"%s\"%s>\n%s\n</agent_result>", result.TaskID, result.AgentName, state, resume, strings.TrimSpace(result.Text)), nil
		},
	})
}
