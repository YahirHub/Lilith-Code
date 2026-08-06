package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommandForShellForcesPowerShellUTF8(t *testing.T) {
	command := `Write-Output "línea 🚀"; exit 3`
	got := commandForShell(command, ShellPowerShell)
	if !strings.HasPrefix(got, powershellUTF8Prelude) {
		t.Fatalf("PowerShell command is missing UTF-8 prelude: %q", got)
	}
	if !strings.HasSuffix(got, command) {
		t.Fatalf("PowerShell command was not kept last: %q", got)
	}
	if strings.Count(got, command) != 1 {
		t.Fatalf("PowerShell command was duplicated: %q", got)
	}
}

func TestCommandForShellLeavesOtherInterpretersUnchanged(t *testing.T) {
	command := `printf 'línea 🚀'`
	for _, kind := range []string{ShellBash, ShellSh, ShellCmd, ShellPortable} {
		if got := commandForShell(command, kind); got != command {
			t.Errorf("shell %s command=%q; want unchanged %q", kind, got, command)
		}
	}
}

func TestRunWindowsPowerShellPreservesUTF8AndExitCode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only PowerShell encoding test")
	}
	command := `[Console]::Out.WriteLine("línea 🚀"); [Console]::Error.WriteLine("error ágil 🧪"); exit 3`
	result, err := Run(context.Background(), Request{
		Command: command,
		Dir:     t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ShellKind != ShellPowerShell {
		t.Fatalf("auto shell=%q; want powershell (result=%+v)", result.ShellKind, result)
	}
	if result.Command != command {
		t.Fatalf("reported command=%q; want original %q", result.Command, command)
	}
	if result.ExitCode != 3 {
		t.Fatalf("exit code=%d; want 3 (result=%+v)", result.ExitCode, result)
	}
	if !utf8.ValidString(result.Stdout) || !strings.Contains(result.Stdout, "línea 🚀") {
		t.Fatalf("stdout is not preserved as UTF-8: %q", result.Stdout)
	}
	if !utf8.ValidString(result.Stderr) || !strings.Contains(result.Stderr, "error ágil 🧪") {
		t.Fatalf("stderr is not preserved as UTF-8: %q", result.Stderr)
	}
}
