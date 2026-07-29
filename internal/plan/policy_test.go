package plan

import "testing"

func TestPlanToolVisibility(t *testing.T) {
	if ToolVisible(Plan, "create_file", true) {
		t.Fatal("mutating file tool leaked into plan mode")
	}
	if ToolVisible(Plan, "todo_write", false) {
		t.Fatal("todo_write should be hidden in plan mode")
	}
	if !ToolVisible(Plan, "run_terminal_command", true) {
		t.Fatal("terminal inspection should remain visible")
	}
}

func TestSafeCommandAllowlist(t *testing.T) {
	allowed := []string{
		"git status --short",
		"git diff --stat",
		"git branch --list",
		"rg plan internal",
		"go env GOPATH GOMOD",
		"go list ./internal/...",
		"node --version",
	}
	for _, command := range allowed {
		if !IsSafeCommand(command) {
			t.Errorf("expected safe: %s", command)
		}
	}
	blocked := []string{
		"git reset --hard HEAD~1",
		"git branch new-branch",
		"go test ./...",
		"go env -w GOPROXY=x",
		"rg --pre cat foo",
		"cat a > b",
		"npm install",
		"rm -rf .",
	}
	for _, command := range blocked {
		if IsSafeCommand(command) {
			t.Errorf("expected blocked: %s", command)
		}
	}
}

func TestBuildHidesPlanOnlyTools(t *testing.T) {
	for _, name := range []string{"plan_question", "plan_exit"} {
		if ToolVisible(Build, name, false) {
			t.Fatalf("%s must not leak into Build", name)
		}
	}
	if !ToolVisible(Build, "todo_write", false) {
		t.Fatal("Build should keep todo_write")
	}
}
