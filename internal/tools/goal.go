package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ligoal "github.com/lilith/li/internal/goal"
)

func init() {
	register(Definition{
		Name:          "create_goal",
		Description:   "Create a durable objective for this long-running session when no goal is currently active. Call it once, then continue the active goal instead of recreating it. Goal execution has no artificial token, step, turn, or time budget. Use when the user explicitly asks for sustained autonomous work or invokes /goal.",
		PromptSnippet: "Create one unlimited durable objective when none is active",
		PromptGuidelines: []string{
			"Never call create_goal repeatedly. Once a goal is active, keep working and use get_goal/update_goal/goal_complete instead.",
		},
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"objective": map[string]any{"type": "string"}}, "required": []string{"objective"}},
		Available: func(env Env) bool {
			if env.Goal == nil {
				return false
			}
			state := env.Goal.Snapshot()
			return state == nil || state.Status == ligoal.Complete
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			s, e := env.Goal.Set(str(args, "objective"))
			if e != nil {
				return "", e
			}
			return goalJSON(s), nil
		},
	})
	register(Definition{Name: "get_goal", Description: "Get the current durable long-running goal, status, approximate usage and elapsed time. Usage is diagnostic only and never stops execution.", PromptSnippet: "Inspect the durable session goal", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}, Available: func(env Env) bool { return env.Goal != nil }, Run: func(_ context.Context, _ map[string]any, env Env) (string, error) {
		s := env.Goal.Snapshot()
		if s == nil {
			return `{"goal":null}`, nil
		}
		return goalJSON(s), nil
	}})
	register(Definition{Name: "update_goal", Description: "Update the current durable goal to active or blocked. Use blocked when a material user decision prevents progress. Completion has a separate explicit goal_complete action with a required final summary.", PromptSnippet: "Mark durable goal active/blocked", Parameters: map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"active", "blocked"}}}, "required": []string{"status"}}, Available: func(env Env) bool {
		if env.Goal == nil {
			return false
		}
		state := env.Goal.Snapshot()
		return state != nil && (state.Status == ligoal.Active || state.Status == ligoal.Blocked)
	}, Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
		status := ligoal.Status(strings.ToLower(str(args, "status")))
		if status != ligoal.Active && status != ligoal.Blocked {
			return "", fmt.Errorf("update_goal accepts active or blocked; use goal_complete to finish")
		}
		if e := env.Goal.UpdateStatus(status); e != nil {
			return "", e
		}
		return goalJSON(env.Goal.Snapshot()), nil
	}})
	register(Definition{Name: "goal_complete", Description: "Explicitly complete the current durable goal only after verifying that its objective is satisfied. Include a concise user-facing summary of the completed result.", PromptSnippet: "Complete the durable goal with a final summary", Parameters: map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string", "description": "Concise final result and validation summary."}}, "required": []string{"summary"}}, Available: func(env Env) bool {
		if env.Goal == nil {
			return false
		}
		state := env.Goal.Snapshot()
		return state != nil && state.Status == ligoal.Active
	}, Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
		if err := env.Goal.Complete(str(args, "summary")); err != nil {
			return "", err
		}
		return goalJSON(env.Goal.Snapshot()), nil
	}})
}
func goalJSON(s *ligoal.State) string { b, _ := json.Marshal(s); return string(b) }
