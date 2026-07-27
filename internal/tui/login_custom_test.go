package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lilith/li/internal/providers"
)

func TestCustomLoginEmptyModelsFetchesAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "gpt-5.4"},
				{"id": "custom-model"},
			},
		})
	}))
	defer server.Close()

	ctx := &AppContext{
		ConfigDir: t.TempDir(),
		Styles:    NewStyles(DefaultTheme()),
	}
	m := NewCustomLogin(ctx)
	m.step = stepModels
	m.name = "Compatible"
	m.url = server.URL + "/v1"
	m.key = "secret"
	m.updateInputForStep()

	next, cmd := m.advance()
	fetching, ok := next.(CustomLoginModel)
	if !ok {
		t.Fatalf("model type = %T", next)
	}
	if !fetching.fetching || cmd == nil {
		t.Fatal("Enter vacío debe iniciar la consulta de /models")
	}

	msg, ok := cmd().(modelsFetchedMsg)
	if !ok {
		t.Fatalf("fetch command message type unexpected")
	}
	if msg.err != nil {
		t.Fatalf("fetch command error = %v", msg.err)
	}

	_, doneCmd := fetching.Update(msg)
	if doneCmd == nil {
		t.Fatal("tras descubrir modelos debe finalizar el login y volver al chat")
	}

	cfg, err := providers.Load(ctx.ConfigDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	p := cfg.FindProvider("compatible")
	if p == nil {
		t.Fatal("el proveedor descubierto no fue persistido")
	}
	if len(p.Models) != 2 || p.Models[0].ID != "gpt-5.4" || p.Models[1].ID != "custom-model" {
		t.Fatalf("models persisted = %+v", p.Models)
	}
	if cfg.ActiveProviderID != p.ID || cfg.ActiveModelID != "gpt-5.4" {
		t.Fatalf("active = %s/%s", cfg.ActiveProviderID, cfg.ActiveModelID)
	}
}

func TestCustomLoginManualModelsKeepsOptionalContextSyntax(t *testing.T) {
	ctx := &AppContext{
		ConfigDir: t.TempDir(),
		Styles:    NewStyles(DefaultTheme()),
	}
	m := NewCustomLogin(ctx)
	m.step = stepModels
	m.name = "Manual"
	m.url = "https://example.com/v1"
	m.input.SetValue("modelo-a=250000, modelo-b")

	_, cmd := m.advance()
	if cmd == nil {
		t.Fatal("la entrada manual válida debe finalizar el login")
	}

	cfg, err := providers.Load(ctx.ConfigDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	p := cfg.FindProvider("manual")
	if p == nil || len(p.Models) != 2 {
		t.Fatalf("provider = %+v", p)
	}
	if p.Models[0].MaxContextTokens != 250000 {
		t.Fatalf("explicit context = %d", p.Models[0].MaxContextTokens)
	}
	if p.Models[1].MaxContextTokens <= 0 {
		t.Fatalf("el contexto omitido debe resolverse automáticamente: %+v", p.Models[1])
	}
}
