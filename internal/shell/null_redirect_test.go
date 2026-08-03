package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeNullRedirects(t *testing.T) {
	tests := map[string]string{
		"go test ./... > null 2>&1":      "go test ./... > /dev/null 2>&1",
		"command 2>null":                 "command 2>/dev/null",
		`command >> "NULL"`:              "command >> /dev/null",
		"command &> null; echo done":     "command &> /dev/null; echo done",
		"command > null 2> null":         "command > /dev/null 2> /dev/null",
		`echo "value > null"`:            `echo "value > null"`,
		"printf null > null.txt":         "printf null > null.txt",
		"mkdir -p src/null && echo done": "mkdir -p src/null && echo done",
	}
	for input, want := range tests {
		if got := normalizeNullRedirects(input, ShellBash); got != want {
			t.Errorf("normalizeNullRedirects(%q, bash)=%q; want %q", input, got, want)
		}
	}
}

func TestRunDoesNotCreateLiteralNullRedirectFile(t *testing.T) {
	if _, err := resolveExecutionShell(ShellBash, "printf x"); err != nil {
		t.Skipf("bash is not available on this host: %v", err)
	}
	root := t.TempDir()
	result, err := Run(context.Background(), Request{Command: "printf x > null", Dir: root, Shell: ShellBash})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%q", result.ExitCode, result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "null")); !os.IsNotExist(err) {
		t.Fatalf("literal null file must not be created; stat err=%v", err)
	}
	if result.Command != "printf x > /dev/null" {
		t.Fatalf("normalized command=%q", result.Command)
	}
}

func TestNormalizeNullRedirectsUsesSelectedShellDevice(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{ShellBash, "command > /dev/null"},
		{ShellPowerShell, "command > $null"},
		{ShellCmd, "command > NUL"},
	}
	for _, test := range tests {
		if got := normalizeNullRedirects("command > null", test.kind); got != test.want {
			t.Errorf("kind %s: got %q; want %q", test.kind, got, test.want)
		}
	}
}
