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

func TestConfigGeneralKeepsSkillCatalogInDedicatedSection(t *testing.T) {
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
	for _, want := range []string{"General", "Skills", "Búsqueda", "Seguridad", "Lilith / LILITH.md"} {
		if !strings.Contains(view, want) {
			t.Fatalf("config view missing %q", want)
		}
	}
	if strings.Contains(view, "skill-que-no-debe-aparecer") {
		t.Fatal("general section must not render the discovered skills list")
	}
}

func TestConfigSkillsSectionTogglesIndividualSkill(t *testing.T) {
	ctx := testConfigContext(t)
	settings := config.Defaults()
	settings.SkillsEnabled = true
	m := &ConfigScreen{
		ctx:      ctx,
		settings: settings,
		section:  configSectionSkills,
		focus:    configSectionNavFocus,
		loaded: []skills.Skill{{
			Name:        "ponytail-development",
			Description: "Professional software methodology.",
			Source:      "builtin",
		}},
		search: newSearchConfigState(ctx),
	}
	view := stripANSI(m.View())
	for _, want := range []string{"Interruptor maestro", "ponytail-development", "origen: interna", "[ON]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("skills section missing %q:\n%s", want, view)
		}
	}

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	if m.focus != "skill:ponytail-development" {
		t.Fatalf("skill focus = %q", m.focus)
	}
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	m = next.(*ConfigScreen)
	if config.IsSkillEnabled(m.settings, "ponytail-development") {
		t.Fatal("skill preference was not disabled")
	}
	persisted, err := config.Load(ctx.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if config.IsSkillEnabled(persisted, "ponytail-development") {
		t.Fatal("disabled skill preference was not persisted")
	}
}

func TestConfigSectionArrowsOnlyWorkWhenTopNavigationHasFocus(t *testing.T) {
	ctx := testConfigContext(t)
	m := NewConfigScreen(ctx)

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyRight})
	m = next.(*ConfigScreen)
	if m.section != configSectionSkills {
		t.Fatalf("right on top nav section = %q, want skills", m.section)
	}
	if m.focus != configSectionNavFocus {
		t.Fatalf("focus = %q, want top navigation", m.focus)
	}

	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyRight})
	m = next.(*ConfigScreen)
	if m.section != configSectionSearch {
		t.Fatalf("second right on top nav section = %q, want search", m.section)
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

func TestConfigViewportFollowsFocusAndReturnsToHeader(t *testing.T) {
	ctx := testConfigContext(t)
	ctx.Height = 12
	m := NewConfigScreen(ctx)

	top := stripANSI(m.View())
	for _, want := range []string{"Configuración", "General", "Skills", "Búsqueda", "Seguridad"} {
		if !strings.Contains(top, want) {
			t.Fatalf("initial config viewport missing %q:\n%s", want, top)
		}
	}
	if m.viewportOffset != 0 {
		t.Fatalf("initial viewport offset = %d, want 0", m.viewportOffset)
	}

	// Move to the final control. The viewport must follow the focused card
	// instead of leaving it below the physical terminal.
	for range 6 {
		next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
		m = next.(*ConfigScreen)
		_ = m.View()
	}
	if m.focus != "back" {
		t.Fatalf("focus = %q, want back", m.focus)
	}
	bottom := stripANSI(m.View())
	if !strings.Contains(bottom, "Volver al chat") {
		t.Fatalf("bottom focused control is outside viewport:\n%s", bottom)
	}
	if m.viewportOffset == 0 {
		t.Fatal("viewport did not move while navigating to the final control")
	}

	// Walk back to the section navigation. The last Up must restore the real
	// beginning of /config, not only change the logical focus off-screen.
	for range 6 {
		next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyUp})
		m = next.(*ConfigScreen)
		_ = m.View()
	}
	if m.focus != configSectionNavFocus {
		t.Fatalf("focus = %q, want section navigation", m.focus)
	}
	returned := stripANSI(m.View())
	for _, want := range []string{"Configuración", "General", "Skills", "Búsqueda", "Seguridad"} {
		if !strings.Contains(returned, want) {
			t.Fatalf("returned config viewport missing %q:\n%s", want, returned)
		}
	}
	if m.viewportOffset != 0 {
		t.Fatalf("returned viewport offset = %d, want 0", m.viewportOffset)
	}
}

func TestConfigSearchViewportKeepsProviderVisibleAndCanReturnToTop(t *testing.T) {
	ctx := testConfigContext(t)
	ctx.Height = 10
	m := NewConfigScreen(ctx)
	m.setSection(configSectionSearch)

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	if m.focus != "search-content" {
		t.Fatalf("focus = %q, want search-content", m.focus)
	}

	for i, provider := range websearch.ProviderIDs {
		if i > 0 {
			next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
			m = next.(*ConfigScreen)
		}
		view := stripANSI(m.View())
		label := websearch.Labels[provider]
		if !strings.Contains(view, label) {
			t.Fatalf("selected provider %q is outside viewport:\n%s", label, view)
		}
	}
	if m.viewportOffset == 0 {
		t.Fatal("search viewport did not move while navigating to the final provider")
	}

	for i := len(websearch.ProviderIDs) - 1; i > 0; i-- {
		next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyUp})
		m = next.(*ConfigScreen)
		_ = m.View()
	}
	// Up once more from the first provider returns to the section picker.
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyUp})
	m = next.(*ConfigScreen)
	returned := stripANSI(m.View())
	if m.focus != configSectionNavFocus {
		t.Fatalf("focus = %q, want section navigation", m.focus)
	}
	if m.viewportOffset != 0 {
		t.Fatalf("returned search viewport offset = %d, want 0", m.viewportOffset)
	}
	if !strings.Contains(returned, "Configuración") || !strings.Contains(returned, "Búsqueda") {
		t.Fatalf("search viewport did not return to top:\n%s", returned)
	}
}

func TestConfigSecurityDownFocusesFirstControl(t *testing.T) {
	ctx := testConfigContext(t)
	m := NewConfigScreen(ctx)
	m.setSection(configSectionSecurity)

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	if m.focus != "ssh-remote" {
		t.Fatalf("security first focus = %q, want ssh-remote", m.focus)
	}
}

func TestSecurityOpensDedicatedSSHRemoteScreenAndPersistsPreset(t *testing.T) {
	ctx := testConfigContext(t)
	m := NewConfigScreen(ctx)
	m.setSection(configSectionSecurity)

	next, _ := m.Update(uikit.KeyMsg{Type: uikit.KeyDown})
	m = next.(*ConfigScreen)
	if m.focus != "ssh-remote" {
		t.Fatalf("security first focus=%q", m.focus)
	}
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	m = next.(*ConfigScreen)
	if !m.sshSecurityOpen {
		t.Fatal("SSH Remote dedicated screen did not open")
	}
	view := stripANSI(m.View())
	for _, want := range []string{"Seguridad > SSH Remoto", "Cambios críticos", "Cada acción", "Sólo comandos", "Confiar en el modelo", "Personalizado", "Permisos personalizados"} {
		if !strings.Contains(view, want) {
			t.Fatalf("SSH security screen missing %q:\n%s", want, view)
		}
	}

	m.focus = "ssh-mode:" + string(config.SSHApprovalTrustModel)
	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	m = next.(*ConfigScreen)
	persisted, err := config.Load(ctx.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SSHRemote.Mode != config.SSHApprovalTrustModel {
		t.Fatalf("persisted SSH mode=%q", persisted.SSHRemote.Mode)
	}

	next, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyEsc})
	m = next.(*ConfigScreen)
	if m.sshSecurityOpen || m.focus != "ssh-remote" {
		t.Fatalf("Esc did not return to Security: open=%v focus=%q", m.sshSecurityOpen, m.focus)
	}
}
