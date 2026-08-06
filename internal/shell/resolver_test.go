package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/lilith/li/internal/toolchain"
)

func TestChooseShellKindUsesNativeWindowsDefault(t *testing.T) {
	available := shellAvailability{bash: true, sh: true, powershell: true, cmd: true}
	got, err := chooseShellKind("windows", "auto", "go test ./...", available)
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellPowerShell {
		t.Fatalf("neutral Windows command shell=%q; want powershell", got)
	}
}

func TestChooseShellKindDetectsWindowsSyntax(t *testing.T) {
	available := shellAvailability{bash: true, sh: true, powershell: true, cmd: true}
	tests := map[string]string{
		`$env:CGO_ENABLED='0'; go build ./...`:    ShellPowerShell,
		`set CGO_ENABLED=0 && go build ./...`:     ShellCmd,
		`CGO_ENABLED=0 go build ./...`:            ShellBash,
		`VALUE=native-bash-ok; printf "$VALUE"`:   ShellBash,
		"VALUE=native-bash-ok\nprintf \"$VALUE\"": ShellBash,
		`ONLY_ASSIGNMENT=native-bash-ok`:          ShellBash,
		`mkdir -p dist && rm -f dist/li`:          ShellBash,
	}
	for command, want := range tests {
		got, err := chooseShellKind("windows", "auto", command, available)
		if err != nil {
			t.Fatalf("%q: %v", command, err)
		}
		if got != want {
			t.Errorf("%q shell=%q; want %q", command, got, want)
		}
	}
}

func TestChooseShellKindHonorsExplicitShell(t *testing.T) {
	available := shellAvailability{bash: true, powershell: true, cmd: true}
	got, err := chooseShellKind("windows", "cmd", `$env:VALUE='x'`, available)
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellCmd {
		t.Fatalf("explicit shell=%q; want cmd", got)
	}
}

func TestChooseShellKindRejectsUnavailableDetectedSyntax(t *testing.T) {
	available := shellAvailability{powershell: true, cmd: true}
	_, err := chooseShellKind("windows", "auto", `mkdir -p dist`, available)
	if err == nil || !strings.Contains(err.Error(), "bash syntax") {
		t.Fatalf("expected unavailable bash syntax error, got %v", err)
	}
}

func TestChooseShellKindFallsBackToPortableForPOSIXSyntax(t *testing.T) {
	got, err := chooseShellKind("windows", "auto", `mkdir -p dist`, shellAvailability{powershell: true, cmd: true, portable: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellPortable {
		t.Fatalf("fallback shell=%q; want portable", got)
	}
}

func TestChooseShellKindFallsBackToPortableOnMinimalUnix(t *testing.T) {
	got, err := chooseShellKind("linux", "auto", "printf ok", shellAvailability{portable: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellPortable {
		t.Fatalf("minimal unix shell=%q; want portable", got)
	}
}

func TestChooseShellKindUsesBashOnUnix(t *testing.T) {
	got, err := chooseShellKind("linux", "auto", "go test ./...", shellAvailability{bash: true, sh: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellBash {
		t.Fatalf("linux shell=%q; want bash", got)
	}
}

func TestNormalizeRequestedShellAliases(t *testing.T) {
	for input, want := range map[string]string{"": ShellAuto, "pwsh": ShellPowerShell, "cmd.exe": ShellCmd, "posix": ShellSh, "embedded": ShellPortable, "gosh": ShellPortable} {
		got, err := normalizeRequestedShell(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if got != want {
			t.Errorf("%q normalized=%q; want %q", input, got, want)
		}
	}
}

func TestRunAutoUsesNativeInterpreter(t *testing.T) {
	var command, wantKind, wantOutput string
	switch runtime.GOOS {
	case "windows":
		command = `Write-Output "native-shell-ok"`
		wantKind = ShellPowerShell
		wantOutput = "native-shell-ok"
	default:
		command = `printf native-shell-ok`
		if _, ok := toolchain.ResolveShell(ShellBash); ok {
			wantKind = ShellBash
		} else {
			wantKind = ShellSh
		}
		wantOutput = "native-shell-ok"
	}
	result, err := Run(context.Background(), Request{Command: command, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.ShellKind != wantKind {
		t.Fatalf("shell kind=%q; want %q", result.ShellKind, wantKind)
	}
	if strings.TrimSpace(result.Stdout) != wantOutput {
		t.Fatalf("stdout=%q; want %q", result.Stdout, wantOutput)
	}
}

func TestRunWindowsExplicitCmd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only execution test")
	}
	result, err := Run(context.Background(), Request{Command: `echo native-cmd-ok`, Dir: t.TempDir(), Shell: ShellCmd})
	if err != nil {
		t.Fatal(err)
	}
	if result.ShellKind != ShellCmd || !strings.Contains(result.Stdout, "native-cmd-ok") {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunWindowsPOSIXSyntaxUsesBash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only execution test")
	}
	if _, ok := toolchain.ResolveShell(ShellBash); !ok {
		t.Skip("Git Bash/bash is not installed")
	}
	result, err := Run(context.Background(), Request{Command: `VALUE=native-bash-ok; printf "$VALUE"`, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.ShellKind != ShellBash || strings.TrimSpace(result.Stdout) != "native-bash-ok" {
		t.Fatalf("result=%+v", result)
	}
}

func TestChooseShellHonorsExplicitPortable(t *testing.T) {
	got, err := chooseShellKind("linux", ShellPortable, "git status", shellAvailability{portable: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != ShellPortable {
		t.Fatalf("got %q; want portable", got)
	}
}

func TestAvailableKindsAlwaysIncludesPortable(t *testing.T) {
	got := AvailableKinds()
	for _, kind := range got {
		if kind == ShellPortable {
			return
		}
	}
	t.Fatalf("portable shell missing from %v", got)
}
