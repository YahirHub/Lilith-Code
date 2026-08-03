package tui

import (
	"testing"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

func TestFilterEnabledSkillsRemovesOnlyConfiguredNames(t *testing.T) {
	t.Parallel()
	settings := config.Defaults()
	config.SetSkillEnabled(&settings, "ponytail-development", false)
	catalog := []skills.Skill{
		{Name: "ponytail-development", Source: "builtin"},
		{Name: "review", Source: "project"},
	}
	got := filterEnabledSkills(settings, catalog)
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("enabled skills = %#v", got)
	}
	if len(catalog) != 2 {
		t.Fatal("filter mutated the source catalog")
	}
}
