package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

func TestConfigUsesCardsAndDoesNotRenderSkillNames(t *testing.T) {
	ctx := &AppContext{
		ConfigDir: t.TempDir(),
		Styles:    NewStyles(DefaultTheme()),
		Width:     100,
		Height:    32,
	}
	m := &ConfigScreen{
		ctx:      ctx,
		settings: config.Settings{SkillsEnabled: true},
		section:  configSectionGeneral,
		focus:    "skills",
		loaded:   []skills.Skill{{Name: "skill-que-no-debe-aparecer"}},
	}
	view := m.View()
	for _, want := range []string{"General", "Búsqueda", "Seguridad", "Skills", "1 skill(s) detectada(s)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("config view missing %q", want)
		}
	}
	if strings.Contains(view, "skill-que-no-debe-aparecer") {
		t.Fatal("config must not render the discovered skills list")
	}
	if strings.Contains(view, "Lista compacta") || strings.Contains(view, "Panel dividido") || strings.Contains(view, "Secciones") {
		t.Fatal("experimental design picker must be removed")
	}
}

func TestConfigTabsOpenDevelopmentSections(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme()), Width: 100, Height: 32}
	m := &ConfigScreen{ctx: ctx, section: configSectionGeneral, focus: "skills"}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*ConfigScreen)
	if m.section != configSectionSearch {
		t.Fatalf("tab section = %q, want search", m.section)
	}
	if !strings.Contains(m.View(), "Características en desarrollo") {
		t.Fatal("search section must show development placeholder")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*ConfigScreen)
	if m.section != configSectionSecurity {
		t.Fatalf("second tab section = %q, want security", m.section)
	}
	if !strings.Contains(m.View(), "Características en desarrollo") {
		t.Fatal("security section must show development placeholder")
	}
}
