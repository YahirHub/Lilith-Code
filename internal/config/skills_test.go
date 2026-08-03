package config

import (
	"reflect"
	"testing"
)

func TestSkillPreferencesDefaultEnabledAndNormalizeDisabledNames(t *testing.T) {
	t.Parallel()
	settings := Defaults()
	if !IsSkillEnabled(settings, "ponytail-development") {
		t.Fatal("new skills must be enabled unless explicitly disabled")
	}

	SetSkillEnabled(&settings, " Ponytail-Development ", false)
	SetSkillEnabled(&settings, "review", false)
	SetSkillEnabled(&settings, "REVIEW", false)
	if IsSkillEnabled(settings, "ponytail-development") {
		t.Fatal("disabled skill reported as enabled")
	}
	if want := []string{"ponytail-development", "review"}; !reflect.DeepEqual(settings.DisabledSkills, want) {
		t.Fatalf("disabled skills = %#v, want %#v", settings.DisabledSkills, want)
	}

	SetSkillEnabled(&settings, "PONYTAIL-DEVELOPMENT", true)
	if !IsSkillEnabled(settings, "ponytail-development") {
		t.Fatal("re-enabled skill reported as disabled")
	}
	if want := []string{"review"}; !reflect.DeepEqual(settings.DisabledSkills, want) {
		t.Fatalf("disabled skills after re-enable = %#v, want %#v", settings.DisabledSkills, want)
	}
}

func TestDisabledSkillsPersistInSettingsJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	settings := Defaults()
	settings.SkillsEnabled = true
	SetSkillEnabled(&settings, "ponytail-development", false)
	if err := Save(dir, settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SkillsEnabled {
		t.Fatal("master skills switch was not persisted")
	}
	if IsSkillEnabled(loaded, "ponytail-development") {
		t.Fatal("disabled skill was not persisted")
	}
	if !loaded.ProjectInstructionsEnabled || !loaded.ClaudeCompatibilityEnabled {
		t.Fatal("loading skill preferences must preserve default settings")
	}
}
