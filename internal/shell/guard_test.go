package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSafetyRejectsIncompleteHeredoc(t *testing.T) {
	err := validateCommandSafety("cat > reporte.md <<'EOF'\n# report\nmissing terminator")
	if err == nil || !strings.Contains(err.Error(), "incomplete shell heredoc") || !strings.Contains(err.Error(), "append_file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommandSafetyAllowsSmallCompleteHeredoc(t *testing.T) {
	if err := validateCommandSafety("cat > tiny.txt <<'EOF'\nhello\nEOF"); err != nil {
		t.Fatalf("small complete heredoc should remain available: %v", err)
	}
}

func TestCommandSafetyRejectsLargeCompleteHeredoc(t *testing.T) {
	command := "cat > reporte.md <<'EOF'\n" + strings.Repeat("section data\n", 700) + "EOF"
	err := validateCommandSafety(command)
	if err == nil || !strings.Contains(err.Error(), "safe") || !strings.Contains(err.Error(), "write_file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommandSafetyRejectsIncompletePowerShellHereString(t *testing.T) {
	command := "powershell -Command \"$body = @'\npartial report\n\""
	err := validateCommandSafety(command)
	if err == nil || !strings.Contains(err.Error(), "PowerShell here-string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommandSafetyAllowsSmallCompletePowerShellHereString(t *testing.T) {
	command := "powershell -Command \"$body = @'\nsmall\n'@\nSet-Content report.md $body\""
	if err := validateCommandSafety(command); err != nil {
		t.Fatalf("small complete here-string should remain available: %v", err)
	}
}

func TestCommandSafetyRejectsLargePowerShellInlineWrite(t *testing.T) {
	command := `powershell -Command "Set-Content -Path report.md -Value '` + strings.Repeat("x", MaxInlineFileWriteCommandBytes) + `'"`
	if err := validateCommandSafety(command); err == nil {
		t.Fatal("large PowerShell inline write should be rejected")
	}
}

func TestCommandSafetyAllowsLongNonWritingCommand(t *testing.T) {
	command := "echo args " + strings.Repeat("package-name ", 700)
	// No output redirection: long argument lists/build commands are not the file
	// truncation hazard this guard targets.
	if err := validateCommandSafety(command); err != nil {
		t.Fatalf("long non-writing command should remain available: %v", err)
	}
}

func TestRunBlocksIncompleteHeredocWithoutCreatingPartialFile(t *testing.T) {
	root := t.TempDir()
	_, err := Run(context.Background(), Request{
		Dir:     root,
		Command: "cat > reporte.md <<'EOF'\n# partial report",
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete shell heredoc") {
		t.Fatalf("expected pre-execution heredoc error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "reporte.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unsafe command created a destination: %v", statErr)
	}
}
