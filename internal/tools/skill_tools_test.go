package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilith/li/internal/skills"
)

func TestSkillToolsSearchListReadAndDiscover(t *testing.T) {
	t.Parallel()
	sk := makeToolSkillFixture(t)
	env := Env{Skills: []skills.Skill{sk}}

	out, err := Execute(context.Background(), "list_skills", map[string]any{"query": "bootstrap"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "bootstrap-ui-design") || strings.Contains(out, "SKILL.md") {
		t.Fatalf("unexpected compact skill catalog:\n%s", out)
	}

	out, err = Execute(context.Background(), "skill_search", map[string]any{
		"skill":         "bootstrap-ui-design",
		"query":         "notification dropdown",
		"kinds":         []any{"asset", "reference"},
		"limit":         float64(3),
		"context_lines": float64(1),
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "assets/snippets/notification-dropdown.html") || !strings.Contains(out, "score=") {
		t.Fatalf("unexpected ranked search output:\n%s", out)
	}

	out, err = Execute(context.Background(), "skill_files", map[string]any{
		"skill": "bootstrap-ui-design",
		"kinds": []any{"script"},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "scripts/audit.py") || strings.Contains(out, "AGENTS.md") {
		t.Fatalf("unexpected resource listing:\n%s", out)
	}

	out, err = Execute(context.Background(), "skill_read", map[string]any{
		"skill":  "bootstrap-ui-design",
		"path":   "references/dropdowns.md",
		"offset": float64(2),
		"limit":  float64(1),
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lines 2-2") || !strings.Contains(out, "Use dropdown-menu-end") || strings.Contains(out, "Line three") {
		t.Fatalf("unexpected bounded read:\n%s", out)
	}
}

func TestToolSearchCanMaterializeSkillTools(t *testing.T) {
	t.Parallel()
	var enabled []string
	env := Env{Materialize: func(names []string) { enabled = append(enabled, names...) }}
	out, err := Execute(context.Background(), "tool_search", map[string]any{"query": "search skill"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(enabled, "skill_search") {
		t.Fatalf("skill_search not materialized: %v\n%s", enabled, out)
	}
	if containsString(enabled, "code_search") {
		t.Fatalf("skill-specific discovery should not materialize project code_search: %v", enabled)
	}
	if !strings.Contains(out, "skill_search") {
		t.Fatalf("tool_search output missing skill_search:\n%s", out)
	}
}

func TestSkillReadLoadsLargeMainSkillByDefault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var body strings.Builder
	for i := 1; i <= 180; i++ {
		fmt.Fprintf(&body, "instruction line %03d\n", i)
	}
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sk := skills.Skill{Name: "large-skill", Description: "Large test skill.", FilePath: path, BaseDir: root, Source: "project"}
	out, err := Execute(context.Background(), "skill_read", map[string]any{"skill": "large-skill"}, Env{Skills: []skills.Skill{sk}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lines 1-180 of 180") || !strings.Contains(out, "instruction line 180") {
		t.Fatalf("main SKILL.md should load completely by default when below the safety cap:\n%s", out)
	}
	if strings.Contains(out, "more available") {
		t.Fatalf("unexpected pagination for 180-line SKILL.md:\n%s", out)
	}
}

func TestWithSkillToolsKeepsSkillBootstrapSurfaceAvailable(t *testing.T) {
	t.Parallel()
	got := WithSkillTools([]string{"tool_search", "read_files"}, true)
	for _, want := range []string{"tool_search", "read_files", "skill_read", "skill_search", "skill_files"} {
		if !containsString(got, want) {
			t.Fatalf("missing %s from skill-aware tool set: %v", want, got)
		}
	}
	without := WithSkillTools([]string{"tool_search"}, false)
	if containsString(without, "skill_read") || containsString(without, "skill_search") || containsString(without, "skill_files") {
		t.Fatalf("skill tools should stay lazy when there are no visible skills: %v", without)
	}
}

func TestSkillToolPromptRequiresMainSkillBeforeSecondarySearch(t *testing.T) {
	t.Parallel()
	lines, guidelines := PromptInfo([]string{"skill_read", "skill_search"})
	joined := strings.Join(append(lines, guidelines...), "\n")
	for _, want := range []string{"SKILL.md", "must be loaded", "not a substitute"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("skill prompt metadata missing %q:\n%s", want, joined)
		}
	}
}

func TestSkillReadRejectsUnknownSkill(t *testing.T) {
	t.Parallel()
	_, err := Execute(context.Background(), "skill_read", map[string]any{"skill": "missing"}, Env{})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected useful missing-skill error, got %v", err)
	}
}

func makeToolSkillFixture(t *testing.T) skills.Skill {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", "---\nname: bootstrap-ui-design\ndescription: Bootstrap dashboards and components.\n---\n# Bootstrap UI\n")
	write("references/dropdowns.md", "# Dropdowns\nUse dropdown-menu-end for right alignment.\nLine three should stay outside a one-line read.\n")
	write("assets/snippets/notification-dropdown.html", "<div class=\"dropdown-menu notification-dropdown\">notification dropdown</div>\n")
	write("scripts/audit.py", "print('audit')\n")
	write("AGENTS.md", "maintainer memory notification dropdown\n")
	return skills.Skill{Name: "bootstrap-ui-design", Description: "Bootstrap dashboards and components.", FilePath: filepath.Join(root, "SKILL.md"), BaseDir: root, Source: "project"}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
