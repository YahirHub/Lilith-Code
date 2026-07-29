package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/providers"
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
