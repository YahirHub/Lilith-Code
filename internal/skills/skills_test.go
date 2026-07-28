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
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated skill, got %d: %#v", len(got), got)
	}
	if got[0].Name != "glass-design" {
		t.Fatalf("unexpected skill name: %q", got[0].Name)
	}
	if got[0].Description != "Project-specific glass design rules." {
		t.Fatalf("project skill should override user skill, got description %q", got[0].Description)
	}
	if got[0].Source != "project" {
		t.Fatalf("expected project source, got %q", got[0].Source)
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
