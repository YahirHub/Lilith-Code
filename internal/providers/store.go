package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lilith/li/internal/config"
)

const FileName = "providers.json"

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func Path(dir string) string { return filepath.Join(dir, FileName) }

// Load reads providers.json (never returns error for missing file).
func Load(dir string) (Config, error) {
	cfg := Config{Version: CurrentVersion, Providers: []Provider{}}
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cfg, nil // ignore corruption
	}
	if loaded.Version == 0 {
		loaded.Version = CurrentVersion
	}
	if loaded.Providers == nil {
		loaded.Providers = []Provider{}
	}
	return loaded, nil
}

// LoadWithBundled returns persisted providers plus bundled ones (OpenCode Free).
func LoadWithBundled(dir string) (Config, error) {
	cfg, err := Load(dir)
	if err != nil {
		return cfg, err
	}
	bundled := BundledProviders()
	// Bundled entries take precedence; skip persisted duplicates with the same id.
	reserved := map[string]bool{}
	for _, b := range bundled {
		reserved[b.ID] = true
	}
	merged := append([]Provider{}, bundled...)
	for _, p := range cfg.Providers {
		if !reserved[p.ID] {
			merged = append(merged, p)
		}
	}
	cfg.Providers = merged
	// Bundled providers are not written to providers.json. Overlay their last
	// successful live catalog so newly released models survive screen changes and
	// process restarts while the network is unavailable.
	if err := applyCatalogCache(dir, &cfg); err != nil {
		return cfg, err
	}
	// If nothing active but user has never configured, default to OpenCode Free.
	if cfg.ActiveProviderID == "" && !fileExists(Path(dir)) && len(cfg.Providers) > 0 {
		cfg.ActiveProviderID = cfg.Providers[0].ID
		if len(cfg.Providers[0].Models) > 0 {
			cfg.ActiveModelID = cfg.Providers[0].Models[0].ID
		}
	}
	// An OAuth/API-key provider may remain in providers.json after its secret is
	// removed or before its first login. Never expose it as the active runtime
	// provider until its connection can actually be verified.
	if err := ReconcileActive(dir, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes providers.json atomically.
func Save(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, config.DirMode); err != nil {
		return err
	}
	cfg.Version = CurrentVersion
	// Drop bundled providers before persisting.
	persist := Config{
		Version:          cfg.Version,
		ActiveProviderID: cfg.ActiveProviderID,
		ActiveModelID:    cfg.ActiveModelID,
		Providers:        []Provider{},
	}
	for _, p := range cfg.Providers {
		if p.Bundled {
			continue
		}
		persist.Providers = append(persist.Providers, p)
	}
	data, err := json.MarshalIndent(persist, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", Path(dir), os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), config.FileMode); err != nil {
		return err
	}
	_ = os.Chmod(tmp, config.FileMode)
	return os.Rename(tmp, Path(dir))
}

// NormalizeBaseURL trims trailing slashes / OpenAI-style suffixes.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("La URL no puede estar vacía.")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("URL inválida: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("La URL debe usar http:// o https://.")
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return "", errors.New("Por seguridad, http:// solo se permite en localhost.")
	}
	u.Fragment = ""
	u.RawQuery = ""
	path := strings.TrimRight(u.Path, "/")
	// Remove trailing /chat/completions if the user pasted the full endpoint.
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/chat/completions"), "/chat/completions/")
	u.Path = path
	return strings.TrimRight(u.String(), "/"), nil
}

func isLoopback(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// NormalizeID validates and lowercases a provider id.
func NormalizeID(input string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(input))
	if !idPattern.MatchString(id) {
		return "", errors.New("El identificador debe usar letras minúsculas, números y guiones (máx 64).")
	}
	return id, nil
}

// SlugFromName derives a provider id from a human name.
func SlugFromName(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "", errors.New("El nombre debe contener al menos una letra o número.")
	}
	if len(slug) > 64 {
		slug = slug[:64]
	}
	return NormalizeID(slug)
}

// ParseModelsInput accepts "id" or "id=ctx" entries separated by comma/newline.
func ParseModelsInput(input string) ([]Model, error) {
	entries := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\n' })
	seen := map[string]bool{}
	out := []Model{}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		m := Model{ID: e}
		if idx := strings.Index(e, "="); idx >= 0 {
			m.ID = strings.TrimSpace(e[:idx])
			ctxStr := strings.ReplaceAll(strings.TrimSpace(e[idx+1:]), "_", "")
			var ctx int
			if _, err := fmt.Sscanf(ctxStr, "%d", &ctx); err != nil || ctx <= 0 {
				return nil, fmt.Errorf("El límite de contexto de %q no es válido.", m.ID)
			}
			m.MaxContextTokens = ctx
		}
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, errors.New("Debes indicar al menos un modelo.")
	}
	return Enrich(out), nil
}

// ValidateEnvName ensures a $VAR reference is well-formed.
func ValidateEnvName(name string) error {
	if !envPattern.MatchString(name) {
		return errors.New("El nombre de variable de entorno no es válido.")
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
