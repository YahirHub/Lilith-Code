package hooks

import "testing"

func TestParseDeny(t *testing.T) {
	r := parseOutput([]byte(`{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"no"}}`))
	if !r.Blocked || r.Reason != "no" {
		t.Fatalf("%+v", r)
	}
}
func TestMatcher(t *testing.T) {
	if !matches("Bash|Edit", "Bash") {
		t.Fatal("expected regex match")
	}
	if matches("Read", "Bash") {
		t.Fatal("unexpected")
	}
}

func TestParseWorktreePath(t *testing.T) {
	r := parseOutput([]byte(`{"hookSpecificOutput":{"hookEventName":"WorktreeCreate","worktreePath":"/tmp/custom-worktree"}}`))
	if r.WorktreePath != "/tmp/custom-worktree" {
		t.Fatalf("unexpected worktree path: %q", r.WorktreePath)
	}
}
