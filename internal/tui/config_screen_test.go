package tui

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
	"github.com/lilith/li/internal/websearch"
)

func testConfigContext(t *testing.T) *AppContext {
	t.Helper()
	return &AppContext{
		ConfigDir: t.TempDir(),
		Styles:    NewStyles(DefaultTheme()),
		Width:     100,
		Height:    36,
	}
}

func TestConfigUsesCardsAndDoesNotRenderSkillNames(t *testing.T) {
	ctx := testConfigContext(t)
	m := &ConfigScreen{
		ctx:      ctx,
		settings: config.Settings{SkillsEnabled: true},
		section:  configSectionGeneral,
		focus:    configSectionNavFocus,
		loaded:   []skills.Skill{{Name: "skill-que-no-debe-aparecer"}},
		search:   newSearchConfigState(ctx),
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
}

func TestConfigSectionArrowsOnlyWorkWhenTopNavigationHasFocus(t *testing.T) {
	ctx := testConfigContext(t)
	m := NewConfigScreen(ctx)

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyRight})
	m = next.(*ConfigScreen)
	if m.section != configSectionSearch {
		t.Fatalf("right on top nav section = %q, want search", m.section)
	}
	if m.focus != configSectionNavFocus {
		t.Fatalf("focus = %q, want top navigation", m.focus)
	}

	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	if m.focus != "search-content" {
		t.Fatalf("down from top nav focus = %q, want search-content", m.focus)
	}

	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyRight})
	m = next.(*ConfigScreen)
	if m.section != configSectionSearch {
		t.Fatalf("right inside search content changed section to %q", m.section)
	}

	// Tavily is the first row, so Up returns to the top section picker.
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyUp})
	m = next.(*ConfigScreen)
	if m.focus != configSectionNavFocus {
		t.Fatalf("up from first provider focus = %q, want top navigation", m.focus)
	}

	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyRight})
	m = next.(*ConfigScreen)
	if m.section != configSectionSecurity {
		t.Fatalf("right after returning to nav section = %q, want security", m.section)
	}
}

func TestSearchSectionStartsAsProviderListAndOpensProviderDetail(t *testing.T) {
	ctx := testConfigContext(t)
	m := NewConfigScreen(ctx)
	m.setSection(configSectionSearch)

	view := m.View()
	for _, want := range []string{"Motores de búsqueda", "Tavily", "Brave Search", "Exa", "SIN CONFIGURAR"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search list missing %q", want)
		}
	}
	if strings.Contains(view, "Configurar API key") || strings.Contains(view, "Ordenar respaldos") {
		t.Fatal("provider actions must not saturate the provider list screen")
	}

	// Enter the list and open the selected provider.
	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	m = next.(*ConfigScreen)
	if m.search.view != searchViewProvider {
		t.Fatalf("search view = %q, want provider detail", m.search.view)
	}
	detail := m.View()
	for _, want := range []string{"Configurar API key", "Probar conexión", "Ordenar respaldos", "Volver a motores"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("provider detail missing %q", want)
		}
	}
}

func TestConfiguredSearchProviderIsGreenAndActiveStateIsVisible(t *testing.T) {
	ctx := testConfigContext(t)
	if err := websearch.SaveAPIKey(ctx.ConfigDir, websearch.Tavily, "tvly-test-key"); err != nil {
		t.Fatal(err)
	}
	if err := websearch.RecordTest(ctx.ConfigDir, websearch.Tavily, true, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := websearch.SetDefault(ctx.ConfigDir, websearch.Tavily); err != nil {
		t.Fatal(err)
	}

	m := NewConfigScreen(ctx)
	m.setSection(configSectionSearch)
	view := m.View()
	greenLabel := ctx.Styles.Success.Bold(true).Render("Tavily")
	if !strings.Contains(view, greenLabel) {
		t.Fatal("configured provider name must use the success/green style")
	}
	if !strings.Contains(view, "[ACTIVO]") {
		t.Fatal("validated enabled provider must show ACTIVO")
	}
	if !strings.Contains(view, "predeterminado") {
		t.Fatal("active default provider must show predeterminado metadata")
	}
}

func TestSetupSearchCommandWasRemoved(t *testing.T) {
	if FindCommand("setup-search") != nil || FindCommand("search-setup") != nil {
		t.Fatal("/setup-search compatibility command must be removed")
	}
	if strings.Contains(helpText(), "/setup-search") {
		t.Fatal("help must not advertise /setup-search")
	}
}
