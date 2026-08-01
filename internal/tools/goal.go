package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ligoal "github.com/lilith/li/internal/goal"
)

func init() {
	register(Definition{Name: "create_goal", Description: "Create or replace the durable objective for this long-running session. Goal execution has no artificial token, step, turn, or time budget. Use when the user explicitly asks for sustained autonomous work or invokes /goal.", PromptSnippet: "Create an unlimited durable long-running objective", Parameters: map[string]any{"type": "object", "properties": map[string]any{"objective": map[string]any{"type": "string"}}, "required": []string{"objective"}}, Available: func(env Env) bool { return env.Goal != nil }, Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
		s, e := env.Goal.Set(str(args, "objective"))
		if e != nil {
			return "", e
		}
		return goalJSON(s), nil
	}})
	register(Definition{Name: "get_goal", Description: "Get the current durable long-running goal, status, approximate usage and elapsed time. Usage is diagnostic only and never stops execution.", PromptSnippet: "Inspect the durable session goal", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}, Available: func(env Env) bool { return env.Goal != nil }, Run: func(_ context.Context, _ map[string]any, env Env) (string, error) {
		s := env.Goal.Snapshot()
		if s == nil {
			return `{"goal":null}`, nil
		}
		return goalJSON(s), nil
	}})
	register(Definition{Name: "update_goal", Description: "Update the status of the current durable goal. Mark complete only when the objective is actually satisfied; use blocked when a material user decision prevents progress.", PromptSnippet: "Mark durable goal progress/status", Parameters: map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"active", "blocked", "complete"}}}, "required": []string{"status"}}, Available: func(env Env) bool { return env.Goal != nil && env.Goal.Snapshot() != nil }, Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
		status := ligoal.Status(strings.ToLower(str(args, "status")))
		if status != ligoal.Active && status != ligoal.Blocked && status != ligoal.Complete {
			return "", fmt.Errorf("model may only set goal status active, blocked or complete")
		}
		if e := env.Goal.UpdateStatus(status); e != nil {
			return "", e
		}
		return goalJSON(env.Goal.Snapshot()), nil
	}})
}
func goalJSON(s *ligoal.State) string { b, _ := json.Marshal(s); return string(b) }
