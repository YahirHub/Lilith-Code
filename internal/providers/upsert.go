package providers

import (
	"errors"
	"fmt"

	"github.com/lilith/li/internal/secrets"
)

// UpsertParams describe a new or updated user-defined provider.
type UpsertParams struct {
	ID              string
	Name            string
	BaseURL         string
	APIKeyInput     string // raw value from the login flow: "sk-…", "env:VAR", "none", ""
	Models          []Model
	Headers         map[string]string
	APIKeyHeader    string
	APIKeyPrefix    string
	UseNonStreaming bool
}

// Upsert creates or updates a provider and persists it. It also writes/removes
// the associated secret in provider-auth.json when a literal API key is given.
func Upsert(dir string, p UpsertParams) (*Provider, error) {
	if p.Name == "" && p.ID == "" {
		return nil, errors.New("Debes indicar el nombre del proveedor.")
	}
	if p.ID == "" {
		slug, err := SlugFromName(p.Name)
		if err != nil {
			return nil, err
		}
		p.ID = slug
	} else {
		id, err := NormalizeID(p.ID)
		if err != nil {
			return nil, err
		}
		p.ID = id
	}
	if p.ID == OpenCodeFreeID {
		return nil, fmt.Errorf("El identificador %q está reservado por Lilith.", p.ID)
	}
	baseURL, err := NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return nil, err
	}
	if len(p.Models) == 0 {
		return nil, errors.New("Debes indicar al menos un modelo.")
	}

	cfg, err := Load(dir)
	if err != nil {
		return nil, err
	}
	auth, err := secrets.Load(dir)
	if err != nil {
		return nil, err
	}

	authKind := AuthNone
	envName := ""
	rawKey := ""
	switch {
	case p.APIKeyInput == "" || p.APIKeyInput == "none":
		authKind = AuthNone
		delete(auth.APIKeys, p.ID)
	case len(p.APIKeyInput) > 4 && (p.APIKeyInput[:4] == "env:" || p.APIKeyInput[:4] == "ENV:"):
		envName = p.APIKeyInput[4:]
		if err := ValidateEnvName(envName); err != nil {
			return nil, err
		}
		authKind = AuthEnv
		delete(auth.APIKeys, p.ID)
	default:
		rawKey = p.APIKeyInput
		authKind = AuthAPIKey
		auth.APIKeys[p.ID] = rawKey
	}

	prov := Provider{
		ID:              p.ID,
		Name:            firstNonEmpty(p.Name, p.ID),
		BaseURL:         baseURL,
		Models:          p.Models,
		Auth:            authKind,
		APIKeyEnv:       envName,
		Headers:         p.Headers,
		APIKeyHeader:    p.APIKeyHeader,
		APIKeyPrefix:    p.APIKeyPrefix,
		UseNonStreaming: p.UseNonStreaming,
	}

	replaced := false
	for i, existing := range cfg.Providers {
		if existing.ID == prov.ID {
			cfg.Providers[i] = prov
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Providers = append(cfg.Providers, prov)
	}
	cfg.ActiveProviderID = prov.ID
	cfg.ActiveModelID = prov.Models[0].ID

	if err := Save(dir, cfg); err != nil {
		return nil, err
	}
	if err := secrets.Save(dir, auth); err != nil {
		return nil, err
	}
	return &prov, nil
}

// SetActive changes the active provider and model.
func SetActive(dir, providerID, modelID string) error {
	cfg, err := LoadWithBundled(dir)
	if err != nil {
		return err
	}
	p := cfg.FindProvider(providerID)
	if p == nil {
		return fmt.Errorf("No existe el proveedor %q.", providerID)
	}
	state, err := ConnectionStateFor(dir, *p)
	if err != nil {
		return err
	}
	if !state.Connected {
		return fmt.Errorf("El proveedor %s no está conectado: %s.", p.Name, state.Reason)
	}
	if modelID == "" && len(p.Models) > 0 {
		modelID = p.Models[0].ID
	}
	if findModel(p.Models, modelID) == nil {
		return fmt.Errorf("El modelo %q no está configurado en %s.", modelID, p.Name)
	}
	persisted, err := Load(dir)
	if err != nil {
		return err
	}
	persisted.ActiveProviderID = providerID
	persisted.ActiveModelID = modelID
	return Save(dir, persisted)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// SetUseNonStreaming updates the transport preference of a persisted custom
// provider without touching its authentication material or model catalog.
func SetUseNonStreaming(dir, providerID string, value bool) error {
	cfg, err := Load(dir)
	if err != nil {
		return err
	}
	p := cfg.FindProvider(providerID)
	if p == nil {
		return fmt.Errorf("No existe el proveedor personalizado %q.", providerID)
	}
	p.UseNonStreaming = value
	return Save(dir, cfg)
}

// Delete removes a persisted custom provider and its API key. Bundled
// providers are intentionally not present in Load(dir), so they cannot be
// deleted through this function.
func Delete(dir, providerID string) error {
	cfg, err := Load(dir)
	if err != nil {
		return err
	}
	idx := -1
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == providerID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("No existe el proveedor personalizado %q.", providerID)
	}
	cfg.Providers = append(cfg.Providers[:idx], cfg.Providers[idx+1:]...)
	if cfg.ActiveProviderID == providerID {
		cfg.ActiveProviderID = ""
		cfg.ActiveModelID = ""
	}
	if err := Save(dir, cfg); err != nil {
		return err
	}
	return secrets.DeleteAPIKey(dir, providerID)
}
