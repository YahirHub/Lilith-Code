package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledDirMaterializesSkillInfrastructureWithoutProductSkills(t *testing.T) {
	t.Parallel()
	configDir := filepath.Join(t.TempDir(), ".li")
	dir := BundledDir(configDir)
	if dir == "" {
		t.Fatal("expected bundled skill cache directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read materialized embedded README: %v", err)
	}
	if !strings.Contains(string(data), "without product-specific built-in") {
		t.Fatalf("unexpected embedded README contents: %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(dir, ".ready")); err != nil {
		t.Fatalf("expected completed cache marker: %v", err)
	}
	for _, removed := range []string{"termux-development", "termux-release"} {
		if _, err := os.Stat(filepath.Join(dir, removed)); !os.IsNotExist(err) {
			t.Fatalf("removed built-in skill %s is still materialized: %v", removed, err)
		}
	}
}

func TestLoadUserAndProjectSkillsOverrideBuiltinByName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	user := filepath.Join(root, ".li", "skills")
	project := filepath.Join(root, "project", ".li", "skills")

	writeNamedSkill(t, builtin, "shared-skill", "Built-in rules.")
	writeNamedSkill(t, user, "shared-skill", "User rules.")

	got := Load(LoadOptions{BuiltinDir: builtin, UserDir: user, ProjectDir: project})
	if len(got) != 1 || got[0].Description != "User rules." || got[0].Source != "user" {
		t.Fatalf("user skill should override builtin: %#v", got)
	}

	writeNamedSkill(t, project, "shared-skill", "Project rules.")
	got = Load(LoadOptions{BuiltinDir: builtin, UserDir: user, ProjectDir: project})
	if len(got) != 1 || got[0].Description != "Project rules." || got[0].Source != "project" {
		t.Fatalf("project skill should override user/builtin: %#v", got)
	}
}

func writeNamedSkill(t *testing.T, root, name, description string) {
	t.Helper()
	mustWriteSkillFile(t, filepath.Join(root, name, "SKILL.md"), "---\nname: "+name+"\ndescription: "+description+"\n---\n# "+name+"\n")
}
