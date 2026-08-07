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

func TestTargetsDoNotPublishBrokenTermuxArtifact(t *testing.T) {
	t.Parallel()
	for _, target := range targets {
		if target.GOOS == "android" || strings.Contains(target.Output, "termux") {
			t.Fatalf("Termux debe compilarse nativamente desde install.sh, target inesperado: %#v", target)
		}
	}
}

func TestTargetsIncludeWindowsCrossCompilation(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"windows/amd64/li-windows-amd64.exe": false,
		"windows/arm64/li-windows-arm64.exe": false,
	}
	for _, target := range targets {
		key := target.GOOS + "/" + target.GOARCH + "/" + target.Output
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("falta target de compilación cruzada: %s", key)
		}
	}
}

func TestParseBuildDistributionAddsPrivateBuildTag(t *testing.T) {
	t.Parallel()
	dist, err := parseBuildDistribution([]string{"--distribution", "company"})
	if err != nil {
		t.Fatal(err)
	}
	if dist != "company" {
		t.Fatalf("distribution=%q", dist)
	}
	if got := strings.Join(buildTags(dist), ","); got != "grammar_set_core,company" {
		t.Fatalf("tags=%q", got)
	}
}

func TestParseBuildDistributionKeepsDefaultPublicBuild(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--distribution=main"}, {"--distribution", "public"}} {
		dist, err := parseBuildDistribution(args)
		if err != nil {
			t.Fatal(err)
		}
		if dist != "default" || strings.Join(buildTags(dist), ",") != "grammar_set_core" {
			t.Fatalf("args=%v dist=%q tags=%v", args, dist, buildTags(dist))
		}
	}
}
