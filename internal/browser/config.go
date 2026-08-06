package browser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func configPath(configDir string) string {
	return filepath.Join(configDir, "browser", "browser.json")
}

func DefaultConfig() Config {
	return Config{Headless: true, ProfileMode: ProfileTemporary}
}

func LoadConfig(configDir string) (Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(configDir) == "" {
		return cfg, errors.New("directorio de configuración de Lilith no disponible")
	}
	data, err := os.ReadFile(configPath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ProfileMode == "" {
		cfg.ProfileMode = ProfileTemporary
	}
	return cfg, nil
}

func SaveConfig(configDir string, cfg Config) error {
	if strings.TrimSpace(configDir) == "" {
		return errors.New("directorio de configuración de Lilith no disponible")
	}
	if cfg.ProfileMode == "" {
		cfg.ProfileMode = ProfileTemporary
	}
	dir := filepath.Dir(configPath(configDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".browser-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, configPath(configDir))
}
