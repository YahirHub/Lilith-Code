package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgent(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeAgentParsingAndPrecedence(t *testing.T) {
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	user := filepath.Join(root, "user")
	project := filepath.Join(root, "project")
	writeAgent(t, builtin, "review.md", "---\nname: reviewer\ndescription: builtin\ntools: Read, Glob, Grep\n---\nbuiltin prompt")
	writeAgent(t, user, "review.md", "---\nname: reviewer\ndescription: user\nmodel: inherit\nskills: [go, testing]\n---\nuser prompt")
	writeAgent(t, project, "review.md", "---\nname: reviewer\ndescription: project\ndisallowedTools:\n  - Bash\nmaxTurns: 8\n---\nproject prompt")
	got := Load(LoadOptions{BuiltinDir: builtin, UserDirs: []string{user}, ProjectDirs: []string{project}})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Description != "project" || got[0].Prompt != "project prompt" || got[0].MaxTurns != 8 {
		t.Fatalf("unexpected: %#v", got[0])
	}
	if len(got[0].DisallowedTools) != 1 || got[0].DisallowedTools[0] != "Bash" {
		t.Fatalf("deny=%v", got[0].DisallowedTools)
	}
}

func TestClaudeAgentModelPreferenceListParsing(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "context7-docs.md", "---\nname: context7-docs\ndescription: docs\nmodel: default, claude-sonnet-4-5, gpt-5.4\n---\nRead docs.")
	got := Load(LoadOptions{UserDirs: []string{dir}})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Model != "default, claude-sonnet-4-5, gpt-5.4" {
		t.Fatalf("model=%q", got[0].Model)
	}
}

func TestOpenCodeFilenameAndPermissions(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "security-auditor.md", "---\ndescription: audits security\nmode: subagent\npermission:\n  edit: deny\n  bash: ask\n---\nAudit only.")
	got := Load(LoadOptions{UserDirs: []string{dir}})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Name != "security-auditor" {
		t.Fatalf("name=%q", got[0].Name)
	}
	if got[0].Permissions["edit"] != "deny" || got[0].Permissions["bash"] != "ask" {
		t.Fatalf("permissions=%v", got[0].Permissions)
	}
}

func TestPrimaryOpenCodeAgentIsNotSubagent(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "build.md", "---\ndescription: build\nmode: primary\n---\nBuild.")
	if got := Load(LoadOptions{UserDirs: []string{dir}}); len(got) != 0 {
		t.Fatalf("expected primary to be filtered: %v", got)
	}
}

func TestFormatForPromptOmitsHiddenAgents(t *testing.T) {
	block := FormatForPrompt([]Agent{
		{Name: "visible", Description: "Visible worker"},
		{Name: "secret", Description: "Hidden worker", Hidden: true},
	})
	if !strings.Contains(block, "visible: Visible worker") {
		t.Fatalf("visible agent missing: %s", block)
	}
	if strings.Contains(block, "secret") || strings.Contains(block, "Hidden worker") {
		t.Fatalf("hidden agent leaked into parent prompt: %s", block)
	}
}

func TestOpenCodeLegacyToolFlags(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "review.md", "---\ndescription: Review only\nmode: subagent\ntools:\n  write: false\n  edit: false\n  bash: false\n---\nReview.")
	got := Load(LoadOptions{UserDirs: []string{dir}})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	for _, name := range []string{"write", "edit", "bash"} {
		if enabled, ok := got[0].ToolFlags[name]; !ok || enabled {
			t.Fatalf("tool flag %s=%v present=%v", name, enabled, ok)
		}
	}
}

func TestBundledTermuxAgentsAreMaterialized(t *testing.T) {
	t.Parallel()
	configDir := filepath.Join(t.TempDir(), ".li")
	dir := BundledDir(configDir)
	if dir == "" {
		t.Fatal("expected bundled agent cache directory")
	}
	for _, name := range []string{"termux-specialist.md", "termux-auditor.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read bundled agent %s: %v", name, err)
		}
		if !strings.Contains(string(data), "Termux") {
			t.Fatalf("bundled agent %s does not contain Termux guidance", name)
		}
	}
}
