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

func TestCreateGoalBecomesUnavailableWhileGoalIsActive(t *testing.T) {
	mgr := ligoal.NewManager(nil)
	if _, err := mgr.Set("finish the project"); err != nil {
		t.Fatal(err)
	}
	names := FilterAvailable([]string{"create_goal", "get_goal", "update_goal"}, Env{Goal: mgr})
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "create_goal") {
		t.Fatalf("create_goal must be hidden after activation: %v", names)
	}
	if !strings.Contains(joined, "get_goal") || !strings.Contains(joined, "update_goal") {
		t.Fatalf("active goal controls disappeared: %v", names)
	}
	if _, err := Execute(context.Background(), "create_goal", map[string]any{"objective": "finish the project"}, Env{Goal: mgr}); err == nil || !strings.Contains(err.Error(), "tool unavailable") {
		t.Fatalf("a repeated create_goal call must be rejected, err=%v", err)
	}
	if err := mgr.UpdateStatus(ligoal.Blocked); err != nil {
		t.Fatal(err)
	}
	names = FilterAvailable([]string{"create_goal"}, Env{Goal: mgr})
	if len(names) != 0 {
		t.Fatalf("a blocked goal must be resolved instead of replaced: %v", names)
	}
	if err := mgr.UpdateStatus(ligoal.Complete); err != nil {
		t.Fatal(err)
	}
	names = FilterAvailable([]string{"create_goal"}, Env{Goal: mgr})
	if len(names) != 1 || names[0] != "create_goal" {
		t.Fatalf("a completed goal should allow a new objective: %v", names)
	}
}
