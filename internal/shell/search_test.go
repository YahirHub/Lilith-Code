package shell

import (
	"context"
	"strings"
	"testing"
)

func TestRepositorySearchGuardRejectsRecursiveGrepWithoutTarget(t *testing.T) {
	command := `grep -rn "func.*ManageConnection\|EventChan"`
	err := validateRepositorySearch(command)
	if err == nil || !strings.Contains(err.Error(), "explicit path") || !strings.Contains(err.Error(), "code_search") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositorySearchGuardAllowsExplicitTarget(t *testing.T) {
	for _, command := range []string{
		`grep -rn "needle" internal`,
		`grep --recursive --regexp "needle" .`,
		`grep -R -f patterns.txt src`,
	} {
		if err := validateRepositorySearch(command); err != nil {
			t.Fatalf("explicit search %q was rejected: %v", command, err)
		}
	}
}

func TestRepositorySearchGuardLeavesComplexShellProgramUntouched(t *testing.T) {
	if err := validateRepositorySearch(`grep -rn "needle" | head -20`); err != nil {
		t.Fatalf("complex command must not be rewritten or preflight-parsed: %v", err)
	}
}

func TestRepositorySearchDetection(t *testing.T) {
	searches := []string{
		`grep -R "needle" .`,
		`rg -n "needle" .`,
		`find . -name '*.go'`,
		`git grep "needle"`,
		`grep -rn "needle" . | head -20`,
	}
	for _, command := range searches {
		if !IsRepositorySearchCommand(command) {
			t.Errorf("expected repository search: %s", command)
		}
	}
	for _, command := range []string{`go test ./...`, `grep "needle" file.go`, `printf hello | grep h`, `go test ./... && rg needle`} {
		if IsRepositorySearchCommand(command) {
			t.Errorf("unexpected repository search: %s", command)
		}
	}
}

func TestRunBlocksRecursiveGrepBeforeProcessCreation(t *testing.T) {
	_, err := Run(context.Background(), Request{Dir: t.TempDir(), Command: `grep -rn "needle"`})
	if err == nil || !strings.Contains(err.Error(), "no command was run") {
		t.Fatalf("expected pre-execution repository search error, got %v", err)
	}
}
