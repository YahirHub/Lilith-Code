package tools

import (
	"context"
	"fmt"

	litodo "github.com/lilith/li/internal/todo"
)

func init() {
	register(Definition{
		Name:          "todo_write",
		Description:   fmt.Sprintf("Maintain the complete task plan atomically. Every call is authoritative: include every task key to keep and omit a key to delete it. Existing tasks may omit unchanged fields; new tasks require subject and status. Supports up to %d tasks.", litodo.MaxTasks),
		PromptSnippet: "Maintain the current multi-step task plan with one atomic update",
		PromptGuidelines: []string{
			"For implementation or investigation work that clearly needs 3 or more meaningful steps, create a todo plan after gathering enough context and before substantial changes.",
			"Each todo_write call is the complete authoritative task list: include every key to keep and omit keys only when that work should disappear from the plan.",
			"Keep task keys stable. Existing tasks may omit unchanged fields; new tasks require subject and status. Reuse the current baseRevision when available.",
			"Use dependsOn only for real prerequisites. Do not start or complete a task while a prerequisite remains unfinished.",
			"Mark tasks completed only after the implementation or verification represented by that task has actually succeeded. Completed tasks are removed at the start of the next user turn unless unfinished work still depends on them.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tasks": map[string]any{
					"type":        "array",
					"maxItems":    litodo.MaxTasks,
					"description": "Complete authoritative list of tasks to retain. Omitted current keys are deleted. Existing tasks may omit unchanged fields.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"key": map[string]any{
								"type":        "string",
								"minLength":   1,
								"maxLength":   40,
								"pattern":     "^[a-z0-9][a-z0-9._-]*$",
								"description": "Stable lowercase task key, for example inspect-api or run-tests.",
							},
							"subject": map[string]any{
								"type":        "string",
								"minLength":   1,
								"maxLength":   160,
								"description": "Short imperative task subject. Required for a new key; omit to preserve the current value.",
							},
							"description": map[string]any{
								"type":        "string",
								"maxLength":   2000,
								"description": "Optional detail. Omit to preserve; use an empty string to clear it.",
							},
							"status": map[string]any{
								"type":        "string",
								"enum":        []string{string(litodo.Pending), string(litodo.InProgress), string(litodo.Completed)},
								"description": "Task status. Required for a new key; omit to preserve the current value.",
							},
							"dependsOn": map[string]any{
								"type":        "array",
								"maxItems":    litodo.MaxDeps,
								"description": "Prerequisite task keys. Omit to preserve; use [] to clear dependencies.",
								"items":       map[string]any{"type": "string"},
							},
						},
						"required": []string{"key"},
					},
				},
				"baseRevision": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "Revision shown in the current todo state. Rejects stale writes when supplied.",
				},
			},
			"required": []string{"tasks"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			if env.Todos == nil {
				return "", fmt.Errorf("todo state is unavailable in this runtime")
			}
			input, err := litodo.DecodeArgs(args)
			if err != nil {
				return "", err
			}
			details, err := env.Todos.Write(input)
			if err != nil {
				return "", err
			}
			return litodo.FormatForModel(details.State), nil
		},
	})
}
