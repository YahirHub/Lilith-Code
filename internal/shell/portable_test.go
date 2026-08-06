package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunPortableExecutesBashSyntaxAndGoPipeline(t *testing.T) {
	t.Setenv("PATH", "")
	res, err := Run(context.Background(), Request{
		Command: `name=world; printf 'hello %s\nalpha\nbeta\n' "$name" | grep beta`,
		Dir:     t.TempDir(),
		Shell:   ShellPortable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShellKind != ShellPortable || res.Shell != portableShellPath {
		t.Fatalf("shell=%q path=%q", res.ShellKind, res.Shell)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "beta") {
		t.Fatalf("result=%+v", res)
	}
}

func TestRunPortableUsesNativeGoSearchFallback(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// Needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "ignored.go"), []byte("Needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Request{Command: `rg -F Needle .`, Dir: root, Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "main.go:2:// Needle") {
		t.Fatalf("result=%+v", res)
	}
	if strings.Contains(res.Stdout, "node_modules") {
		t.Fatalf("dependency directory should be skipped: %q", res.Stdout)
	}
}

func TestRunPortableSupportsRedirectionAndFileToolbox(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	res, err := Run(context.Background(), Request{
		Command: `mkdir -p out; printf 'hello\n' > out/a.txt; cp out/a.txt out/b.txt; cat out/b.txt`,
		Dir:     root,
		Shell:   ShellPortable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "hello" {
		t.Fatalf("result=%+v", res)
	}
	data, err := os.ReadFile(filepath.Join(root, "out", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("copied content=%q", data)
	}
}

func TestRunPortableReportsMissingExternalCommand(t *testing.T) {
	t.Setenv("PATH", "")
	res, err := Run(context.Background(), Request{Command: `git status`, Dir: t.TempDir(), Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 127 || !strings.Contains(res.Stderr, "command not found") {
		t.Fatalf("result=%+v", res)
	}
}

func TestRunPortableHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	res, err := Run(ctx, Request{Command: `while true; do :; done`, Dir: t.TempDir(), Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Canceled || res.ExitCode != -1 {
		t.Fatalf("result=%+v", res)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation took too long: %s", elapsed)
	}
}

func TestRunPortablePrefersNativeExecutableWhenAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX executable script")
	}
	bin := t.TempDir()
	helper := filepath.Join(bin, "rg")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf 'native-rg\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	res, err := Run(context.Background(), Request{Command: `rg anything .`, Dir: t.TempDir(), Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "native-rg" {
		t.Fatalf("native executable did not keep priority: %+v", res)
	}
}

func TestRunPortableTimeoutStopsInterpreter(t *testing.T) {
	res, err := Run(context.Background(), Request{
		Command: `while true; do :; done`,
		Dir:     t.TempDir(),
		Shell:   ShellPortable,
		Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Fatalf("result=%+v", res)
	}
}

func TestRunPortableRejectsUnsupportedFallbackOptions(t *testing.T) {
	t.Setenv("PATH", "")
	res, err := Run(context.Background(), Request{Command: `rg -g '*.go' -g '*.md' needle .`, Dir: t.TempDir(), Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 2 || !strings.Contains(res.Stderr, "multiple globs require native ripgrep") {
		t.Fatalf("result=%+v", res)
	}

	res, err = Run(context.Background(), Request{Command: `ls -z`, Dir: t.TempDir(), Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 2 || !strings.Contains(res.Stderr, "unsupported option") {
		t.Fatalf("result=%+v", res)
	}
}

func TestRunPortableRGUsesGlobalLimitAcrossTargets(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("needle\nneedle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Run(context.Background(), Request{Command: `rg -F --max-count 2 needle a.txt b.txt`, Dir: root, Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || strings.Count(res.Stdout, "needle") != 2 {
		t.Fatalf("result=%+v", res)
	}
	if !strings.Contains(res.Stderr, "results truncated at 2 matches") {
		t.Fatalf("expected global truncation notice: %+v", res)
	}
}

func TestRunPortableRGHiddenRequiresFlag(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".hidden.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Request{Command: `rg -F needle .`, Dir: root, Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || strings.Contains(res.Stdout, ".hidden.txt") {
		t.Fatalf("hidden file should be skipped: %+v", res)
	}
	res, err = Run(context.Background(), Request{Command: `rg --hidden -F needle .`, Dir: root, Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, ".hidden.txt:1:needle") {
		t.Fatalf("hidden flag result=%+v", res)
	}
}

func TestRunPortableCopyRejectsDirectoryIntoItself(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Request{Command: `cp -r src src/nested`, Dir: root, Shell: ShellPortable})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.Stderr, "inside itself") {
		t.Fatalf("result=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested destination should not be created, stat err=%v", err)
	}
}
