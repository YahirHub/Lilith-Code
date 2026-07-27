// Package secrets stores provider API keys and OAuth tokens under 0600 perms.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lilith/li/internal/config"
)

const AuthFile = "provider-auth.json"

// Store holds per-provider API keys and OAuth tokens.
type Store struct {
	Version int                    `json:"version"`
	APIKeys map[string]string      `json:"apiKeys"`
	OAuth   map[string]OAuthTokens `json:"oauth,omitempty"`
}

// OAuthTokens is the persisted OAuth state for providers using device flow.
type OAuthTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	TokenType    string `json:"tokenType,omitempty"`
	// AccountID identifica la cuenta ChatGPT/Codex (header
	// `chatgpt-account-id`). Se extrae del id_token JWT al iniciar sesión.
	AccountID string `json:"accountId,omitempty"`
}


func path(dir string) string { return filepath.Join(dir, AuthFile) }

// Load reads provider-auth.json.
func Load(dir string) (Store, error) {
	s := Store{Version: 1, APIKeys: map[string]string{}, OAuth: map[string]OAuthTokens{}}
	data, err := os.ReadFile(path(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{Version: 1, APIKeys: map[string]string{}, OAuth: map[string]OAuthTokens{}}, nil
	}
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	if s.OAuth == nil {
		s.OAuth = map[string]OAuthTokens{}
	}
	return s, nil
}

// Save writes provider-auth.json atomically at 0600.
func Save(dir string, s Store) error {
	if err := os.MkdirAll(dir, config.DirMode); err != nil {
		return err
	}
	s.Version = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path(dir), os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), config.FileMode); err != nil {
		return err
	}
	_ = os.Chmod(tmp, config.FileMode)
	return os.Rename(tmp, path(dir))
}

// SetAPIKey persists an API key for a provider (overwriting).
func SetAPIKey(dir, providerID, key string) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	s.APIKeys[providerID] = key
	return Save(dir, s)
}

// DeleteAPIKey removes an API key.
func DeleteAPIKey(dir, providerID string) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	delete(s.APIKeys, providerID)
	return Save(dir, s)
}
