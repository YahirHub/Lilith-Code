package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/secrets"
)

const (
	defaultCatalogTimeout     = 12 * time.Second
	defaultCodexClientVersion = "0.130.0"
	codexPackageMetadataURL   = "https://registry.npmjs.org/@openai/codex/latest"
)

// RefreshOptions allows tests and embedders to control network behavior.
type RefreshOptions struct {
	HTTPClient         *http.Client
	CodexClientVersion string
}

// RefreshReport summarizes an automatic catalog refresh. Errors are per
// provider so one unavailable endpoint never prevents other catalogs from
// updating.
type RefreshReport struct {
	Updated map[string]int
	Errors  map[string]error
}

func (r RefreshReport) UpdatedCount() int { return len(r.Updated) }

// RefreshConnectedModels refreshes every connected provider in parallel. The
// endpoint catalog is authoritative: newly advertised models are added and
// models no longer returned disappear. Existing local metadata is preserved
// when the remote payload omits it. Custom providers are persisted; bundled
// providers remain runtime-owned and are refreshed again on next startup.
func RefreshConnectedModels(ctx context.Context, dir string, cfg Config, opts RefreshOptions) (Config, RefreshReport) {
	report := RefreshReport{Updated: map[string]int{}, Errors: map[string]error{}}
	states, err := ConnectionStates(dir, cfg)
	if err != nil {
		report.Errors["credentials"] = err
		return cfg, report
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultCatalogTimeout}
	}
	codexVersion := strings.TrimSpace(opts.CodexClientVersion)
	if codexVersion == "" {
		for _, provider := range cfg.Providers {
			if provider.ID == ChatGPTCodexID && states[provider.ID].Connected {
				codexVersion = resolveCodexClientVersion(ctx, client)
				break
			}
		}
	}

	type result struct {
		index  int
		models []Model
		err    error
	}
	results := make(chan result, len(cfg.Providers))
	var wg sync.WaitGroup
	for index, provider := range cfg.Providers {
		if !states[provider.ID].Connected {
			continue
		}
		index, provider := index, provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			models, fetchErr := FetchProviderModels(ctx, dir, provider, client, codexVersion)
			results <- result{index: index, models: models, err: fetchErr}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	originalActiveProvider := cfg.ActiveProviderID
	originalActiveModel := cfg.ActiveModelID
	customChanged := false
	bundledChanged := false
	for item := range results {
		provider := &cfg.Providers[item.index]
		if item.err != nil {
			report.Errors[provider.ID] = item.err
			continue
		}
		models := mergeRemoteModelMetadata(item.models, provider.Models)
		if len(models) == 0 {
			report.Errors[provider.ID] = errors.New("el catálogo remoto no devolvió modelos")
			continue
		}
		if !modelsEqual(provider.Models, models) {
			provider.Models = models
			report.Updated[provider.ID] = len(models)
			if provider.Bundled {
				bundledChanged = true
			} else {
				customChanged = true
			}
		}
	}

	if err := ReconcileActive(dir, &cfg); err != nil {
		report.Errors["active"] = err
	}
	activeChanged := cfg.ActiveProviderID != originalActiveProvider || cfg.ActiveModelID != originalActiveModel
	if customChanged || activeChanged {
		if err := Save(dir, cfg); err != nil {
			report.Errors["persist"] = err
		}
	}
	if bundledChanged {
		if err := saveCatalogCache(dir, cfg); err != nil {
			report.Errors["catalog-cache"] = err
		}
	}
	return cfg, report
}

// FetchProviderModels discovers one provider's live catalog. OpenAI-compatible
// providers use GET {baseURL}/models with their configured headers. ChatGPT
// Codex uses its authenticated account-scoped catalog endpoint.
func FetchProviderModels(ctx context.Context, dir string, provider Provider, client *http.Client, codexClientVersion string) ([]Model, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultCatalogTimeout}
	}
	if provider.ID == ChatGPTCodexID {
		return fetchCodexModels(ctx, dir, provider, client, codexClientVersion)
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if err := applyCatalogAuth(req, dir, provider); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultar %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("%s respondió HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	models, err := decodeModelCatalog(resp.Body)
	if err != nil {
		return nil, err
	}
	if provider.ID == OpenCodeFreeID {
		free := models[:0]
		for _, model := range models {
			if strings.HasSuffix(strings.ToLower(model.ID), "-free") {
				free = append(free, model)
			}
		}
		models = free
	}
	return Enrich(models), nil
}

func applyCatalogAuth(req *http.Request, dir string, provider Provider) error {
	for key, value := range provider.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Lilith-Code/model-catalog")
	}

	var credential string
	switch provider.Auth {
	case AuthNone, AuthBundled:
		return nil
	case AuthEnv:
		credential = strings.TrimSpace(os.Getenv(provider.APIKeyEnv))
		if credential == "" {
			return fmt.Errorf("la variable %s no está definida", provider.APIKeyEnv)
		}
	case AuthAPIKey:
		store, err := secrets.Load(dir)
		if err != nil {
			return err
		}
		credential = strings.TrimSpace(store.APIKeys[provider.ID])
		if credential == "" {
			return fmt.Errorf("no hay API key guardada para %s", provider.Name)
		}
	case AuthOAuth:
		store, err := secrets.Load(dir)
		if err != nil {
			return err
		}
		tokens := store.OAuth[provider.ID]
		credential = strings.TrimSpace(tokens.AccessToken)
		if credential == "" {
			return fmt.Errorf("sesión OAuth ausente para %s", provider.Name)
		}
	}
	if credential == "" {
		return nil
	}
	header := strings.TrimSpace(provider.APIKeyHeader)
	if header == "" {
		header = "Authorization"
	}
	prefix := provider.APIKeyPrefix
	if prefix == "" && strings.EqualFold(header, "Authorization") {
		prefix = "Bearer "
	}
	req.Header.Set(header, prefix+credential)
	return nil
}

func fetchCodexModels(ctx context.Context, dir string, provider Provider, client *http.Client, clientVersion string) ([]Model, error) {
	store, err := secrets.Load(dir)
	if err != nil {
		return nil, err
	}
	tokens, ok := store.OAuth[provider.ID]
	if !ok || strings.TrimSpace(tokens.AccessToken) == "" {
		return nil, fmt.Errorf("sesión OAuth ausente para %s", provider.Name)
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(clientVersion) != "" {
		query := parsed.Query()
		query.Set("client_version", strings.TrimSpace(clientVersion))
		parsed.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex_cli_rs/"+firstNonEmpty(clientVersion, defaultCodexClientVersion))
	if tokens.AccountID != "" {
		req.Header.Set("chatgpt-account-id", tokens.AccountID)
	}
	for key, value := range provider.Headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultar catálogo Codex: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("catálogo Codex respondió HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	models, err := decodeModelCatalog(resp.Body)
	if err != nil {
		return nil, err
	}
	return Enrich(models), nil
}

var codexVersionCache struct {
	sync.Mutex
	value     string
	expiresAt time.Time
}

func resolveCodexClientVersion(ctx context.Context, client *http.Client) string {
	if value := strings.TrimSpace(os.Getenv("LILITH_CODEX_CLIENT_VERSION")); value != "" {
		return value
	}
	codexVersionCache.Lock()
	if codexVersionCache.value != "" && time.Now().Before(codexVersionCache.expiresAt) {
		value := codexVersionCache.value
		codexVersionCache.Unlock()
		return value
	}
	codexVersionCache.Unlock()

	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, codexPackageMetadataURL, nil)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		resp, doErr := client.Do(req)
		if doErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var payload struct {
					Version string `json:"version"`
				}
				if json.NewDecoder(resp.Body).Decode(&payload) == nil && strings.TrimSpace(payload.Version) != "" {
					value := strings.TrimSpace(payload.Version)
					codexVersionCache.Lock()
					codexVersionCache.value = value
					codexVersionCache.expiresAt = time.Now().Add(6 * time.Hour)
					codexVersionCache.Unlock()
					return value
				}
			}
		}
	}
	return defaultCodexClientVersion
}

func decodeModelCatalog(reader io.Reader) ([]Model, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("la respuesta de /models no es un JSON válido")
	}
	entries := catalogEntries(payload)
	seen := map[string]bool{}
	models := make([]Model, 0, len(entries))
	for _, entry := range entries {
		model, ok := modelFromCatalogEntry(entry)
		if !ok || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, errors.New("el endpoint no devolvió ningún modelo reconocible")
	}
	return models, nil
}

func catalogEntries(payload any) []any {
	switch value := payload.(type) {
	case []any:
		return value
	case map[string]any:
		for _, key := range []string{"models", "data", "items", "available_models"} {
			if list, ok := value[key].([]any); ok {
				return list
			}
		}
	}
	return nil
}

func modelFromCatalogEntry(entry any) (Model, bool) {
	if id, ok := entry.(string); ok {
		id = strings.TrimSpace(id)
		return Model{ID: id}, id != ""
	}
	value, ok := entry.(map[string]any)
	if !ok {
		return Model{}, false
	}
	if nested, ok := value["model"].(map[string]any); ok {
		for key, nestedValue := range nested {
			if _, exists := value[key]; !exists {
				value[key] = nestedValue
			}
		}
	}
	id := firstString(value, "id", "slug", "model_slug", "modelId", "model_id")
	if id == "" {
		if modelName, ok := value["model"].(string); ok {
			id = strings.TrimSpace(modelName)
		}
	}
	if id == "" {
		return Model{}, false
	}
	return Model{
		ID:               id,
		Name:             firstString(value, "display_name", "displayName", "name", "title"),
		MaxContextTokens: firstPositiveInt(value, "context_window", "contextWindow", "max_context_tokens", "maxContextTokens", "context_length", "contextLength"),
		MaxOutputTokens:  firstPositiveInt(value, "max_output_tokens", "maxOutputTokens", "output_token_limit", "outputTokenLimit"),
	}, true
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func firstPositiveInt(value map[string]any, keys ...string) int {
	for _, key := range keys {
		switch number := value[key].(type) {
		case float64:
			if number > 0 {
				return int(number)
			}
		case json.Number:
			if parsed, err := strconv.Atoi(number.String()); err == nil && parsed > 0 {
				return parsed
			}
		case string:
			clean := strings.ReplaceAll(strings.TrimSpace(number), "_", "")
			if parsed, err := strconv.Atoi(clean); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

func mergeRemoteModelMetadata(remote, previous []Model) []Model {
	byID := make(map[string]Model, len(previous))
	for _, model := range previous {
		byID[model.ID] = model
	}
	out := make([]Model, 0, len(remote))
	for _, model := range remote {
		if old, ok := byID[model.ID]; ok {
			if model.Name == "" {
				model.Name = old.Name
			}
			if model.MaxContextTokens <= 0 {
				model.MaxContextTokens = old.MaxContextTokens
			}
			if model.MaxOutputTokens <= 0 {
				model.MaxOutputTokens = old.MaxOutputTokens
			}
		}
		out = append(out, model)
	}
	return Enrich(out)
}

func modelsEqual(a, b []Model) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
