package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSupportsFoldedDescriptionAndRecursiveCompatiblePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configDir := filepath.Join(root, ".li")
	project := filepath.Join(root, "project")

	userSkill := filepath.Join(root, ".claude", "skills", "plugins", "ui", "glass-design")
	mustWriteSkillFile(t, filepath.Join(userSkill, "SKILL.md"), `---
name: glass-design
description: >
  Use for responsive glass
  dashboards and components.
---
# Glass
`)
	projectSkill := filepath.Join(project, ".li", "skills", "glass-design")
	mustWriteSkillFile(t, filepath.Join(projectSkill, "SKILL.md"), `---
name: glass-design
description: Project-specific glass design rules.
---
# Project Glass
`)

	got := Load(DefaultLoadOptions(configDir, project))
	var glass *Skill
	count := 0
	for i := range got {
		if got[i].Name == "glass-design" {
			glass = &got[i]
			count++
		}
	}
	if count != 1 || glass == nil {
		t.Fatalf("expected one deduplicated glass-design skill, got %d in %#v", count, got)
	}
	if glass.Description != "Project-specific glass design rules." {
		t.Fatalf("project skill should override user skill, got description %q", glass.Description)
	}
	if glass.Source != "project" {
		t.Fatalf("expected project source, got %q", glass.Source)
	}
}

func TestParseFrontmatterFoldedDescription(t *testing.T) {
	t.Parallel()
	fm := parseFrontmatter(`---
name: bootstrap-ui-design
description: >
  Use for Bootstrap dashboards,
  forms, cards, and responsive layouts.
when_to_use: |
  Bootstrap tasks
  UI refactors
---
body
`)
	if got := fm["description"]; got != "Use for Bootstrap dashboards, forms, cards, and responsive layouts." {
		t.Fatalf("unexpected folded description: %q", got)
	}
	if got := fm["when_to_use"]; got != "Bootstrap tasks\nUI refactors" {
		t.Fatalf("unexpected literal block: %q", got)
	}
}

func TestFormatForPromptRequiresMatchingSkillBeforeProjectWork(t *testing.T) {
	t.Parallel()
	block := FormatForPrompt([]Skill{{Name: "demo", Description: "Use for demo tasks.", FilePath: "/tmp/demo/SKILL.md", BaseDir: "/tmp/demo"}})
	for _, want := range []string{
		"The following skills provide specialized instructions for specific tasks.",
		"you MUST load that skill's SKILL.md with skill_read before inspecting the project",
		"Skills are mandatory when applicable, not optional hints.",
		"do not claim to follow a skill that you have not loaded",
		"skill_search",
		"skill_files",
		"<location>/tmp/demo/SKILL.md</location>",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "<path>") {
		t.Fatalf("prompt should use pi/Agent Skills compatible <location>, got:\n%s", block)
	}
}

func TestFormatInvocationUsesPiStyleSkillEnvelope(t *testing.T) {
	t.Parallel()
	sk := Skill{Name: "demo", FilePath: "/tmp/demo/SKILL.md", BaseDir: "/tmp/demo"}
	got := FormatInvocation(sk, "# Demo\nFollow these rules.", "Fix the dashboard.")
	for _, want := range []string{
		`<skill name="demo" location="/tmp/demo/SKILL.md">`,
		"References are relative to /tmp/demo.",
		"# Demo\nFollow these rules.",
		"</skill>\n\nFix the dashboard.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill invocation missing %q:\n%s", want, got)
		}
	}
}

func mustWriteSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyClaudeOverridesRespectsTrustAndVisibility(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"skillOverrides":{"review":"name-only"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"skillOverrides":{"review":"off"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []Skill{{Name: "review", Description: "Review code"}}
	got := ApplyClaudeOverrides(cfg, root, false, base)
	if len(got) != 1 || got[0].Visibility != "name-only" {
		t.Fatalf("unexpected untrusted override: %#v", got)
	}
	got = ApplyClaudeOverrides(cfg, root, true, base)
	if len(got) != 0 {
		t.Fatalf("trusted project off should hide skill: %#v", got)
	}
}

func TestDefaultSkillScanSkipsClaudePluginNamespaceRoots(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "quality")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"), []byte(`{"name":"quality"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\nReview."), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(LoadOptions{UserDirs: []string{root}}); len(got) != 0 {
		t.Fatalf("plugin skill leaked without namespace: %#v", got)
	}
}

func TestLoadLegacyCommandDirsAcceptsIndividualFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deploy.md")
	if err := os.WriteFile(path, []byte("Deploy the application."), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadLegacyCommandDirs([]string{path}, "user")
	if len(got) != 1 || got[0].Name != "deploy" || got[0].FilePath != path {
		t.Fatalf("commands=%#v", got)
	}
}
