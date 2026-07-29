package websearch

import (
	"strings"
	"time"
)

type ProviderID string

const (
	Tavily    ProviderID = "tavily"
	Brave     ProviderID = "brave"
	Exa       ProviderID = "exa"
	Linkup    ProviderID = "linkup"
	Firecrawl ProviderID = "firecrawl"
	SerpAPI   ProviderID = "serpapi"
	Zenserp   ProviderID = "zenserp"
)

var ProviderIDs = []ProviderID{Tavily, Brave, Exa, Linkup, Firecrawl, SerpAPI, Zenserp}

var Labels = map[ProviderID]string{
	Tavily:    "Tavily",
	Brave:     "Brave Search",
	Exa:       "Exa",
	Linkup:    "Linkup",
	Firecrawl: "Firecrawl",
	SerpAPI:   "SerpApi",
	Zenserp:   "Zenserp",
}

type TestStatus struct {
	OK       bool      `json:"ok"`
	Message  string    `json:"message"`
	TestedAt time.Time `json:"testedAt"`
}

type ProviderSettings struct {
	Enabled  *bool       `json:"enabled,omitempty"`
	LastTest *TestStatus `json:"lastTest,omitempty"`
}

type Settings struct {
	Version         int                             `json:"version"`
	DefaultProvider ProviderID                      `json:"defaultProvider,omitempty"`
	FallbackOrder   []ProviderID                    `json:"fallbackOrder"`
	Providers       map[ProviderID]ProviderSettings `json:"providers"`
}

type Auth struct {
	Version int                   `json:"version"`
	APIKeys map[ProviderID]string `json:"apiKeys"`
}

type State struct {
	APIKey         string
	Configured     bool
	EnabledByUser  bool
	Validated      bool
	Available      bool
	DisabledReason string
	LastTest       *TestStatus
}

func DefaultSettings() Settings {
	return Settings{Version: 1, FallbackOrder: append([]ProviderID(nil), ProviderIDs...), Providers: map[ProviderID]ProviderSettings{}}
}

func DefaultAuth() Auth {
	return Auth{Version: 1, APIKeys: map[ProviderID]string{}}
}

func ValidProvider(id ProviderID) bool {
	for _, candidate := range ProviderIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func NormalizeOrder(values []ProviderID) []ProviderID {
	seen := map[ProviderID]bool{}
	out := make([]ProviderID, 0, len(ProviderIDs))
	for _, id := range values {
		if !ValidProvider(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range ProviderIDs {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}

func NormalizeSettings(s Settings) Settings {
	s.Version = 1
	if s.Providers == nil {
		s.Providers = map[ProviderID]ProviderSettings{}
	}
	if !ValidProvider(s.DefaultProvider) {
		s.DefaultProvider = ""
	}
	s.FallbackOrder = NormalizeOrder(s.FallbackOrder)
	return s
}

func NormalizeAuth(a Auth) Auth {
	a.Version = 1
	if a.APIKeys == nil {
		a.APIKeys = map[ProviderID]string{}
	}
	for id, key := range a.APIKeys {
		if !ValidProvider(id) || strings.TrimSpace(key) == "" {
			delete(a.APIKeys, id)
			continue
		}
		a.APIKeys[id] = strings.TrimSpace(key)
	}
	return a
}

func Resolve(id ProviderID, settings Settings, auth Auth) State {
	ps := settings.Providers[id]
	key := strings.TrimSpace(auth.APIKeys[id])
	configured := key != ""
	enabledByUser := ps.Enabled == nil || *ps.Enabled
	validated := ps.LastTest != nil && ps.LastTest.OK
	state := State{
		APIKey:        key,
		Configured:    configured,
		EnabledByUser: enabledByUser,
		Validated:     validated,
		LastTest:      ps.LastTest,
	}
	switch {
	case !configured:
		state.DisabledReason = "missing-credential"
	case !enabledByUser:
		state.DisabledReason = "disabled-by-user"
	case !validated:
		state.DisabledReason = "not-validated"
	default:
		state.Available = true
	}
	return state
}

func AvailableOrder(settings Settings, auth Auth) []ProviderID {
	order := NormalizeOrder(append([]ProviderID{settings.DefaultProvider}, settings.FallbackOrder...))
	out := make([]ProviderID, 0, len(order))
	for _, id := range order {
		if Resolve(id, settings, auth).Available {
			out = append(out, id)
		}
	}
	return out
}

func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "sin configurar"
	}
	r := []rune(key)
	if len(r) <= 8 {
		if len(r) <= 4 {
			return "••••"
		}
		return string(r[:2]) + "••••" + string(r[len(r)-2:])
	}
	return string(r[:4]) + "••••••" + string(r[len(r)-4:])
}
