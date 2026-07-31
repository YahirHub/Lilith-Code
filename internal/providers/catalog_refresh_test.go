package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lilith/li/internal/secrets"
)

func TestRefreshConnectedModelsDiscoversAndPersistsCustomCatalog(t *testing.T) {
	dir := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/models" {
			t.Errorf("ruta = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Tenant"); got != "lilith" {
			t.Errorf("X-Tenant = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"modelo-nuevo","context_window":262144},{"id":"modelo-dos"}]}`))
	}))
	defer server.Close()

	cfg := Config{Version: CurrentVersion, ActiveProviderID: "custom", ActiveModelID: "modelo-viejo", Providers: []Provider{{
		ID: "custom", Name: "Custom", BaseURL: server.URL + "/v1", Auth: AuthAPIKey,
		Headers: map[string]string{"X-Tenant": "lilith"}, Models: []Model{{ID: "modelo-viejo"}},
	}}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if err := secrets.SetAPIKey(dir, "custom", "secret"); err != nil {
		t.Fatal(err)
	}

	got, report := RefreshConnectedModels(context.Background(), dir, cfg, RefreshOptions{HTTPClient: server.Client()})
	if len(report.Errors) != 0 {
		t.Fatalf("errores = %#v", report.Errors)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
	provider := got.FindProvider("custom")
	if provider == nil || len(provider.Models) != 2 || provider.Models[0].ID != "modelo-nuevo" {
		t.Fatalf("catálogo actualizado = %#v", provider)
	}
	if provider.Models[0].MaxContextTokens != 262144 {
		t.Fatalf("contexto = %d", provider.Models[0].MaxContextTokens)
	}
	if got.ActiveModelID != "modelo-nuevo" {
		t.Fatalf("modelo activo reconciliado = %q", got.ActiveModelID)
	}
	persisted, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p := persisted.FindProvider("custom"); p == nil || len(p.Models) != 2 {
		t.Fatalf("catálogo no persistido = %#v", p)
	}
}

func TestRefreshConnectedModelsSkipsDisconnectedProvider(t *testing.T) {
	dir := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"should-not-load"}]}`))
	}))
	defer server.Close()

	cfg := Config{Providers: []Provider{{
		ID: "needs-key", Name: "Needs key", BaseURL: server.URL, Auth: AuthAPIKey, Models: []Model{{ID: "cached"}},
	}}}
	got, report := RefreshConnectedModels(context.Background(), dir, cfg, RefreshOptions{HTTPClient: server.Client()})
	if requests.Load() != 0 {
		t.Fatalf("un proveedor desconectado hizo %d solicitudes", requests.Load())
	}
	if len(report.Errors) != 0 {
		t.Fatalf("omitir desconectado no debe ser error: %#v", report.Errors)
	}
	if got.Providers[0].Models[0].ID != "cached" {
		t.Fatal("se alteró la caché de un proveedor desconectado")
	}
}

func TestRefreshCodexUsesAccountCatalogAndPersistsBundledCache(t *testing.T) {
	dir := t.TempDir()
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("chatgpt-account-id"); got != "acct_123" {
			t.Errorf("account header = %q", got)
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "9.9.9") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-next-codex","context_window":400000}]}`))
	}))
	defer server.Close()

	store, _ := secrets.Load(dir)
	store.OAuth[ChatGPTCodexID] = secrets.OAuthTokens{AccessToken: "codex-token", RefreshToken: "refresh", AccountID: "acct_123"}
	if err := secrets.Save(dir, store); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Providers: []Provider{{
		ID: ChatGPTCodexID, Name: ChatGPTCodexName, BaseURL: server.URL, Auth: AuthOAuth, Bundled: true,
		Models: []Model{{ID: "old"}},
	}}}
	got, report := RefreshConnectedModels(context.Background(), dir, cfg, RefreshOptions{HTTPClient: server.Client(), CodexClientVersion: "9.9.9"})
	if len(report.Errors) != 0 {
		t.Fatalf("errores = %#v", report.Errors)
	}
	if query.Get("client_version") != "9.9.9" {
		t.Fatalf("client_version = %q", query.Get("client_version"))
	}
	if got.Providers[0].Models[0].ID != "gpt-next-codex" {
		t.Fatalf("modelos = %#v", got.Providers[0].Models)
	}
	cache, err := loadCatalogCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cached := cache.Providers[ChatGPTCodexID]; len(cached) != 1 || cached[0].ID != "gpt-next-codex" {
		t.Fatalf("cache Codex = %#v", cached)
	}
}

func TestLoadWithBundledAppliesLiveCatalogCache(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Providers: []Provider{{
		ID: ChatGPTCodexID, Bundled: true, Models: []Model{{ID: "gpt-future"}},
	}}}
	if err := saveCatalogCache(dir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadWithBundled(dir)
	if err != nil {
		t.Fatal(err)
	}
	codex := loaded.FindProvider(ChatGPTCodexID)
	if codex == nil || len(codex.Models) != 1 || codex.Models[0].ID != "gpt-future" {
		t.Fatalf("cache no aplicado = %#v", codex)
	}
}

func TestRefreshConnectedModelsKeepsManualCatalogWhenEndpointIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := Config{
		Version:          CurrentVersion,
		ActiveProviderID: "manual",
		ActiveModelID:    "modelo-manual",
		Providers: []Provider{{
			ID:      "manual",
			Name:    "Manual",
			BaseURL: server.URL + "/v1",
			Auth:    AuthNone,
			Models:  []Model{{ID: "modelo-manual", MaxContextTokens: 123456}},
		}},
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	got, report := RefreshConnectedModels(context.Background(), dir, cfg, RefreshOptions{HTTPClient: server.Client()})
	if len(report.Errors) != 0 {
		t.Fatalf("un /models ausente no debe registrarse como error: %#v", report.Errors)
	}
	if !report.Unsupported["manual"] || report.UnsupportedCount() != 1 {
		t.Fatalf("unsupported = %#v", report.Unsupported)
	}
	provider := got.FindProvider("manual")
	if provider == nil || len(provider.Models) != 1 || provider.Models[0].ID != "modelo-manual" {
		t.Fatalf("se perdió el catálogo manual: %#v", provider)
	}
	if provider.Models[0].MaxContextTokens != 123456 {
		t.Fatalf("se perdió metadata manual: %#v", provider.Models[0])
	}
	if got.ActiveProviderID != "manual" || got.ActiveModelID != "modelo-manual" {
		t.Fatalf("active = %s/%s", got.ActiveProviderID, got.ActiveModelID)
	}
}
