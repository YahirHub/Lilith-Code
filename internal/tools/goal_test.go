package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ligoal "github.com/lilith/li/internal/goal"
)

func TestCreateGoalHasNoArtificialBudget(t *testing.T) {
	schemas := Schemas([]string{"create_goal"})
	if len(schemas) != 1 {
		t.Fatalf("create_goal schema count=%d", len(schemas))
	}
	data, err := json.Marshal(schemas[0].Function.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "token_budget") {
		t.Fatalf("create_goal must not expose token_budget: %s", data)
	}

	mgr := ligoal.NewManager(nil)
	if _, err := Execute(context.Background(), "create_goal", map[string]any{
		"objective":    "finish the project",
		"token_budget": 1,
	}, Env{Goal: mgr}); err != nil {
		t.Fatal(err)
	}
	mgr.AddUsage(10_000_000)
	if got := mgr.Snapshot().Status; got != ligoal.Active {
		t.Fatalf("goal stopped after diagnostic usage: %q", got)
	}
}
