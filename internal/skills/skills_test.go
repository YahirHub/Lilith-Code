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

func TestFormatForPromptPointsToBoundedSkillTools(t *testing.T) {
	t.Parallel()
	block := FormatForPrompt([]Skill{{Name: "demo", Description: "Demo skill", FilePath: "/tmp/demo/SKILL.md"}})
	for _, want := range []string{"skill_search", "skill_read", "skill_files", "Avoid loading large skill resources wholesale"} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, block)
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
