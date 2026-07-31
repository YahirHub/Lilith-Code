package tui

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/tui/uikit"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/secrets"
)

func TestSubsequenceMatch(t *testing.T) {
	cases := []struct {
		hay, needle string
		wantOK      bool
	}{
		{"gpt-4o", "gpt", true},
		{"gpt-4o", "g4o", true},
		{"gpt-4o", "xyz", false},
		{"claude-3-5-sonnet", "c35s", true},
		{"foo", "", true},
	}
	for _, c := range cases {
		_, ok := subsequenceMatch(c.hay, c.needle)
		if ok != c.wantOK {
			t.Errorf("subsequenceMatch(%q, %q) = %v, want %v", c.hay, c.needle, ok, c.wantOK)
		}
	}
}

func TestModelSelectorAppliesSelectionImmediatelyInMemoryAndOnDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := providers.Config{
		Version:          providers.CurrentVersion,
		ActiveProviderID: "custom",
		ActiveModelID:    "gpt-5.5",
		Providers: []providers.Provider{{
			ID:      "custom",
			Name:    "Custom",
			BaseURL: "https://example.com/v1",
			Auth:    providers.AuthNone,
			Models: []providers.Model{
				{ID: "gpt-5.5"},
				{ID: "deepseek-v4-flash"},
			},
		}},
	}
	if err := providers.Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ctx := &AppContext{ConfigDir: dir, Providers: cfg, Styles: NewStyles(DefaultTheme()), Width: 100, Height: 30}
	m := NewModelSelector(ctx)
	for i, row := range m.filtered {
		if row.modelID == "deepseek-v4-flash" {
			m.cursor = i
			break
		}
	}
	_, cmd := m.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	if cmd == nil {
		t.Fatal("seleccionar un modelo debe volver al chat")
	}
	if got := ctx.Providers.ActiveModelID; got != "deepseek-v4-flash" {
		t.Fatalf("selección en memoria = %q", got)
	}
	persisted, err := providers.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := persisted.ActiveModelID; got != "deepseek-v4-flash" {
		t.Fatalf("selección persistida = %q", got)
	}
}

func TestModelSelectorRendersProviderModelAndContextOnOneLine(t *testing.T) {
	cfg := providers.Config{
		Version:          providers.CurrentVersion,
		ActiveProviderID: "opencode",
		ActiveModelID:    "deepseek-v4-flash",
		Providers: []providers.Provider{{
			ID:   "opencode",
			Name: "Opencode",
			Models: []providers.Model{{
				ID:               "deepseek-v4-flash",
				MaxContextTokens: 1_000_000,
			}},
		}},
	}
	ctx := &AppContext{Providers: cfg, Styles: NewStyles(DefaultTheme()), Width: 120, Height: 30}
	m := NewModelSelector(ctx)
	view := m.View()
	if !strings.Contains(view, "Opencode · deepseek-v4-flash · 1M ctx") {
		t.Fatalf("model row missing compact provider/model/context line: %q", view)
	}
}

func TestModelSelectorOcultaCodexHastaCompletarOAuth(t *testing.T) {
	dir := t.TempDir()
	cfg := providers.Config{
		ActiveProviderID: providers.OpenCodeFreeID,
		ActiveModelID:    "free",
		Providers: []providers.Provider{
			{ID: providers.OpenCodeFreeID, Name: providers.OpenCodeFreeName, Auth: providers.AuthBundled, Models: []providers.Model{{ID: "free"}}},
			{ID: providers.ChatGPTCodexID, Name: providers.ChatGPTCodexName, Auth: providers.AuthOAuth, Models: []providers.Model{{ID: "codex-model"}}},
		},
	}
	ctx := &AppContext{ConfigDir: dir, Providers: cfg, Styles: NewStyles(DefaultTheme()), Width: 100, Height: 30}
	m := NewModelSelector(ctx)
	for _, row := range m.all {
		if row.providerID == providers.ChatGPTCodexID {
			t.Fatal("Codex no debe aparecer antes de iniciar OAuth")
		}
	}

	store, err := secrets.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.OAuth[providers.ChatGPTCodexID] = secrets.OAuthTokens{AccessToken: "token"}
	if err := secrets.Save(dir, store); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
	found := false
	for _, row := range m.all {
		found = found || row.providerID == providers.ChatGPTCodexID
	}
	if !found {
		t.Fatal("Codex debe aparecer después de iniciar OAuth")
	}
}

func TestModelSelectorPermiteFiltrarConRYReservaCtrlRParaActualizar(t *testing.T) {
	dir := t.TempDir()
	cfg := providers.Config{Providers: []providers.Provider{{
		ID: "local", Name: "Proveedor", BaseURL: "http://127.0.0.1:11434/v1", Auth: providers.AuthNone,
		Models: []providers.Model{{ID: "reasoner"}},
	}}}
	ctx := &AppContext{ConfigDir: dir, Providers: cfg, Styles: NewStyles(DefaultTheme()), Width: 100, Height: 30}
	m := NewModelSelector(ctx)
	m.refreshing = false

	next, cmd := m.Update(uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune{'r'}})
	m = next.(ModelSelectorModel)
	if cmd != nil {
		t.Fatal("escribir r en el filtro no debe iniciar una actualización")
	}
	if got := m.filter.Value(); got != "r" {
		t.Fatalf("filtro después de escribir r = %q", got)
	}

	next, cmd = m.Update(uikit.KeyMsg{Type: uikit.KeyCtrlR})
	m = next.(ModelSelectorModel)
	if cmd == nil || !m.refreshing {
		t.Fatal("Ctrl+R debe iniciar la actualización manual del catálogo")
	}
	if got := m.filter.Value(); got != "r" {
		t.Fatalf("Ctrl+R no debe modificar el filtro: %q", got)
	}
}
