package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	planstate "github.com/lilith/li/internal/plan"
)

func init() {
	questionItem := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Stable short identifier for the question.",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The decision question to show the user.",
			},
			"options": map[string]any{
				"type":     "array",
				"maxItems": 6,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"label"},
				},
			},
		},
		"required": []string{"id", "question"},
	}

	register(Definition{
		Name: "plan_question",
		Description: "Ask the user one to three material clarification questions while Plan mode is active. " +
			"Use this only when the decision cannot be resolved by inspecting the project. Each question may provide concise options. " +
			"Do not use this tool to ask whether the final plan is approved; use plan_exit when the plan is complete.",
		PromptSnippet: "Ask 1-3 material clarification questions during Plan mode",
		PromptGuidelines: []string{
			"Use plan_question only for decisions that materially change the implementation and cannot be discovered from code or documentation.",
			"After plan_question, stop. Lilith will collect the answers interactively one question at a time and resume the same Plan turn only after all answers are submitted.",
		},
		Available: func(env Env) bool { return env.Plan != nil && env.AgentMode == planstate.Plan },
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type":     "array",
					"minItems": 1,
					"maxItems": 3,
					"items":    questionItem,
				},
			},
			"required": []string{"questions"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			if env.Plan == nil || env.AgentMode != planstate.Plan {
				return "", errors.New("plan_question is only available in Plan mode")
			}
			questions, err := decodePlanQuestions(args["questions"])
			if err != nil {
				return "", err
			}
			state, err := env.Plan.SetQuestionsFor(env.AgentMode, questions)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("PLAN_QUESTION_PENDING revision=%d questions=%d", state.Revision, len(state.Questions)), nil
		},
	})

	register(Definition{
		Name: "plan_exit",
		Description: "Finish Plan mode research by submitting the complete decision-ready implementation plan for user review. " +
			"Call this alone as the final action only after important questions are resolved. Do not implement anything after this call.",
		PromptSnippet: "Submit the final implementation plan and wait for the user to switch to Build",
		PromptGuidelines: []string{
			"plan_exit must be the final standalone action of a completed planning turn.",
			"The plan must name critical files/components, ordered changes, validation steps and important risks or compatibility constraints.",
		},
		Available: func(env Env) bool { return env.Plan != nil && env.AgentMode == planstate.Plan },
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan": map[string]any{
					"type":        "string",
					"description": "Complete implementation plan in concise Markdown.",
				},
			},
			"required": []string{"plan"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			if env.Plan == nil || env.AgentMode != planstate.Plan {
				return "", errors.New("plan_exit is only available in Plan mode")
			}
			state, err := env.Plan.CompleteFor(env.AgentMode, str(args, "plan"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("PLAN_READY revision=%d", state.Revision), nil
		},
	})
}

func decodePlanQuestions(raw any) ([]planstate.Question, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var questions []planstate.Question
	if err := json.Unmarshal(data, &questions); err != nil {
		return nil, fmt.Errorf("invalid questions: %w", err)
	}
	if len(questions) == 0 {
		return nil, errors.New("questions must contain 1-3 items")
	}
	for i := range questions {
		questions[i].ID = strings.TrimSpace(questions[i].ID)
		questions[i].Question = strings.TrimSpace(questions[i].Question)
	}
	return questions, nil
}
