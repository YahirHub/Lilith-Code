// Package providers defines the provider catalog and persistence.
package providers

// Model is one selectable model inside a Provider.
type Model struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	MaxOutputTokens  int    `json:"maxOutputTokens,omitempty"`
	MaxContextTokens int    `json:"maxContextTokens,omitempty"`
}

// AuthKind describes how the provider authenticates.
type AuthKind string

const (
	AuthNone    AuthKind = "none"    // local endpoints (Ollama, etc.)
	AuthAPIKey  AuthKind = "api_key" // stored in secrets store
	AuthEnv     AuthKind = "env"     // read from env var at request time
	AuthOAuth   AuthKind = "oauth"   // ChatGPT Codex device flow
	AuthBundled AuthKind = "bundled" // OpenCode Free (bundled catalog)
)

// Provider is an OpenAI-compatible endpoint definition.
type Provider struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	BaseURL                  string            `json:"baseUrl"`
	Models                   []Model           `json:"models"`
	Auth                     AuthKind          `json:"auth"`
	APIKeyEnv                string            `json:"apiKeyEnv,omitempty"`
	Headers                  map[string]string `json:"headers,omitempty"`
	APIKeyHeader             string            `json:"apiKeyHeader,omitempty"` // default "Authorization"
	APIKeyPrefix             string            `json:"apiKeyPrefix,omitempty"` // default "Bearer "
	SupportsStructuredOutput bool              `json:"supportsStructuredOutputs,omitempty"`
	UseNonStreaming          bool              `json:"useNonStreaming,omitempty"`
	Bundled                  bool              `json:"-"`
}

// Config is the top-level providers.json shape.
type Config struct {
	Version          int        `json:"version"`
	ActiveProviderID string     `json:"activeProviderId,omitempty"`
	ActiveModelID    string     `json:"activeModelId,omitempty"`
	Providers        []Provider `json:"providers"`
}

const CurrentVersion = 1

// FindProvider returns the provider with matching id (or nil).
func (c *Config) FindProvider(id string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

// ActiveSnapshot describes the current provider+model selection.
type ActiveSnapshot struct {
	ProviderID   string
	ProviderName string
	ModelID      string
	ModelName    string
}

// Active returns a snapshot of the currently selected provider+model.
func (c *Config) Active() ActiveSnapshot {
	p := c.FindProvider(c.ActiveProviderID)
	if p == nil {
		return ActiveSnapshot{}
	}
	m := findModel(p.Models, c.ActiveModelID)
	if m == nil && len(p.Models) > 0 {
		m = &p.Models[0]
	}
	snap := ActiveSnapshot{ProviderID: p.ID, ProviderName: p.Name}
	if m != nil {
		snap.ModelID = m.ID
		snap.ModelName = m.Name
		if snap.ModelName == "" {
			snap.ModelName = m.ID
		}
	}
	return snap
}

func findModel(models []Model, id string) *Model {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}
