package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledDirMaterializesPonytailDevelopmentSkill(t *testing.T) {
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
	if !strings.Contains(string(data), "ponytail-development") {
		t.Fatalf("unexpected embedded README contents: %q", string(data))
	}
	skillData, err := os.ReadFile(filepath.Join(dir, "ponytail-development", "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized Ponytail skill: %v", err)
	}
	for _, want := range []string{"name: ponytail-development", "# Metodología universal", "# 27. Regla de oro"} {
		if !strings.Contains(string(skillData), want) {
			t.Fatalf("Ponytail skill missing %q", want)
		}
	}
	loaded := Load(LoadOptions{BuiltinDir: dir})
	ponytail := Find(loaded, "ponytail-development")
	if ponytail == nil {
		t.Fatalf("materialized Ponytail skill not discovered: %#v", loaded)
	}
	if ponytail.Source != "builtin" || !ponytail.UserInvocable || ponytail.Model != "inherit" {
		t.Fatalf("unexpected Ponytail metadata: %#v", *ponytail)
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

func TestBundledModularDevelopmentSkillsAreMaterialized(t *testing.T) {
	t.Parallel()
	configDir := filepath.Join(t.TempDir(), ".li")
	dir := BundledDir(configDir)
	if dir == "" {
		t.Fatal("expected bundled skill cache directory")
	}

	cases := []struct {
		name      string
		reference string
		contains  string
	}{
		{name: "git-github", reference: "references/history-rewrite-recovery.md", contains: "force-with-lease"},
		{name: "docker-development", reference: "references/compose.md", contains: "docker compose"},
		{name: "frontend-development", reference: "references/browser-audit.md", contains: "frontend-browser-auditor"},
	}
	loaded := Load(LoadOptions{BuiltinDir: dir})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sk := Find(loaded, tc.name)
			if sk == nil {
				t.Fatalf("bundled skill %s not discovered: %#v", tc.name, loaded)
			}
			if sk.Source != "builtin" || !sk.UserInvocable || sk.Model != "inherit" {
				t.Fatalf("unexpected metadata for %s: %#v", tc.name, *sk)
			}
			index, err := os.ReadFile(filepath.Join(dir, tc.name, "SKILL.md"))
			if err != nil {
				t.Fatalf("read %s index: %v", tc.name, err)
			}
			if !strings.Contains(string(index), tc.reference) {
				t.Fatalf("%s index does not route to %s", tc.name, tc.reference)
			}
			ref, err := os.ReadFile(filepath.Join(dir, tc.name, filepath.FromSlash(tc.reference)))
			if err != nil {
				t.Fatalf("read %s resource: %v", tc.reference, err)
			}
			if !strings.Contains(string(ref), tc.contains) {
				t.Fatalf("%s resource missing %q", tc.reference, tc.contains)
			}
		})
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
