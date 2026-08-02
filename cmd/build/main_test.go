package main

import (
	"strings"
	"testing"
)

func TestParseActionDefaultsToBuild(t *testing.T) {
	t.Parallel()
	action, args, err := parseAction(nil)
	if err != nil {
		t.Fatal(err)
	}
	if action != "build" || len(args) != 0 {
		t.Fatalf("unexpected default action: %q %#v", action, args)
	}
}

func TestParseActionAcceptsVersion(t *testing.T) {
	t.Parallel()
	action, args, err := parseAction([]string{"version"})
	if err != nil {
		t.Fatal(err)
	}
	if action != "version" || len(args) != 0 {
		t.Fatalf("unexpected version action: %q %#v", action, args)
	}
	if version, err := projectVersion(); err != nil || version == "" {
		t.Fatalf("project version invalid: version=%q err=%v", version, err)
	}
}

func TestParseActionPreservesToolchainSubcommands(t *testing.T) {
	t.Parallel()
	action, args, err := parseAction([]string{"install", "-f", "--dir", "tools"})
	if err != nil {
		t.Fatal(err)
	}
	if action != "install" || strings.Join(args, " ") != "-f --dir tools" {
		t.Fatalf("unexpected parsed action: %q %#v", action, args)
	}
}

func TestSanitizedBuildEnvRemovesCrossCompilationOverrides(t *testing.T) {
	t.Parallel()
	env := []string{"PATH=/bin", "GOOS=darwin", "GOARCH=386", "GOARM=5", "CGO_ENABLED=1", "KEEP=yes"}
	got := sanitizedBuildEnv(env)
	joined := "\n" + strings.Join(got, "\n") + "\n"
	for _, forbidden := range []string{"\nGOOS=", "\nGOARCH=", "\nGOARM=", "\nCGO_ENABLED="} {
		if strings.Contains(strings.ToUpper(joined), strings.ToUpper(forbidden)) {
			t.Fatalf("sanitized env still contains %s: %#v", forbidden, got)
		}
	}
	if !strings.Contains(joined, "\nKEEP=yes\n") {
		t.Fatalf("unrelated env var was removed: %#v", got)
	}
}

func TestTargetsIncludeNativeTermuxARM64(t *testing.T) {
	t.Parallel()
	for _, target := range targets {
		if target.GOOS == "android" && target.GOARCH == "arm64" && target.Output == "li-termux-arm64" {
			if target.GOARM != "" {
				t.Fatalf("Termux ARM64 target must not set GOARM: %#v", target)
			}
			return
		}
	}
	t.Fatal("missing native android/arm64 Termux target")
}
