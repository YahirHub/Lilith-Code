package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/lilith/li/internal/secrets"
)

// ConnectionState describes whether a provider can currently be used. A
// provider can stay visible in /providers while its models remain hidden from
// /models until the required credential or OAuth session exists.
type ConnectionState struct {
	Connected bool
	Reason    string
}

// ConnectionStates resolves every provider with a single read of the secrets
// store. Providers without credentials (bundled/free or AuthNone) are treated
// as connected once configured.
func ConnectionStates(dir string, cfg Config) (map[string]ConnectionState, error) {
	store, err := secrets.Load(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ConnectionState, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		out[provider.ID] = connectionState(provider, store)
	}
	return out, nil
}

// ConnectionStateFor resolves one provider. Errors reading the credential
// store are returned instead of accidentally exposing models from a provider
// whose connection cannot be verified.
func ConnectionStateFor(dir string, provider Provider) (ConnectionState, error) {
	store, err := secrets.Load(dir)
	if err != nil {
		return ConnectionState{}, err
	}
	return connectionState(provider, store), nil
}

func connectionState(provider Provider, store secrets.Store) ConnectionState {
	switch provider.Auth {
	case AuthBundled, AuthNone, "":
		return ConnectionState{Connected: true}
	case AuthAPIKey:
		if strings.TrimSpace(store.APIKeys[provider.ID]) != "" {
			return ConnectionState{Connected: true}
		}
		return ConnectionState{Reason: "API key no configurada"}
	case AuthEnv:
		name := strings.TrimSpace(provider.APIKeyEnv)
		if name == "" {
			return ConnectionState{Reason: "variable de entorno no configurada"}
		}
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return ConnectionState{Connected: true}
		}
		return ConnectionState{Reason: fmt.Sprintf("falta la variable %s", name)}
	case AuthOAuth:
		tokens, ok := store.OAuth[provider.ID]
		if ok && (strings.TrimSpace(tokens.AccessToken) != "" || strings.TrimSpace(tokens.RefreshToken) != "") {
			return ConnectionState{Connected: true}
		}
		return ConnectionState{Reason: "sesión OAuth no iniciada"}
	default:
		return ConnectionState{Reason: "método de autenticación desconocido"}
	}
}

// ConnectedProviders returns only providers whose connection is currently
// usable. Their original order is preserved.
func ConnectedProviders(dir string, cfg Config) ([]Provider, error) {
	states, err := ConnectionStates(dir, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if states[provider.ID].Connected {
			out = append(out, provider)
		}
	}
	return out, nil
}

// ReconcileActive keeps the active provider/model inside the connected,
// selectable catalog. It mutates only the in-memory config; callers decide
// when that selection should be persisted.
func ReconcileActive(dir string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	states, err := ConnectionStates(dir, *cfg)
	if err != nil {
		return err
	}
	active := cfg.FindProvider(cfg.ActiveProviderID)
	if active != nil && states[active.ID].Connected && len(active.Models) > 0 {
		if findModel(active.Models, cfg.ActiveModelID) == nil {
			cfg.ActiveModelID = active.Models[0].ID
		}
		return nil
	}
	cfg.ActiveProviderID = ""
	cfg.ActiveModelID = ""
	for i := range cfg.Providers {
		provider := &cfg.Providers[i]
		if !states[provider.ID].Connected || len(provider.Models) == 0 {
			continue
		}
		cfg.ActiveProviderID = provider.ID
		cfg.ActiveModelID = provider.Models[0].ID
		break
	}
	return nil
}
