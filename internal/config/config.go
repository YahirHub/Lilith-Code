// Package config manages the ~/.li directory and user settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirName                  = ".li"
	SettingsFile             = "settings.json"
	CurrentOnboardingVersion = 1
	DirMode                  = 0o700
	FileMode                 = 0o600
)

// Settings persisted to ~/.li/settings.json.
type Settings struct {
	OnboardingVersion int    `json:"onboardingVersion"`
	Theme             string `json:"theme,omitempty"`
	// SkillsEnabled activa la carga de skills (Claude Agent Skills) desde
	// ~/.li/skills y ./.li/skills. Off por defecto: sólo se cargan cuando
	// el usuario lo activa desde /config.
	SkillsEnabled bool `json:"skillsEnabled,omitempty"`
}

// Dir returns the config directory path (~/.li), creating it if missing.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	d := filepath.Join(home, DirName)
	if err := os.MkdirAll(d, DirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", d, err)
	}
	// Best-effort permissions fix (no-op on Windows).
	_ = os.Chmod(d, DirMode)
	return d, nil
}

// SettingsPath returns the absolute path to settings.json.
func SettingsPath(dir string) string {
	return filepath.Join(dir, SettingsFile)
}

// Load reads settings.json. Missing file returns zero-value Settings without error.
func Load(dir string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupted file: return defaults so onboarding can rewrite it.
		return Settings{}, nil
	}
	return s, nil
}

// Save writes settings.json atomically with 0600 permissions.
func Save(dir string, s Settings) error {
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(SettingsPath(dir), data, FileMode)
}

// writeAtomic writes to <path>.tmp then renames.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		// Windows: not fatal.
		_ = err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = dir
	return nil
}

// Complete marks first-run onboarding as done.
func Complete(dir string) error {
	s, _ := Load(dir)
	s.OnboardingVersion = CurrentOnboardingVersion
	return Save(dir, s)
}
