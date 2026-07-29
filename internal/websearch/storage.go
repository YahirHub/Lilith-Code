package websearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lilith/li/internal/config"
)

const (
	SettingsFile = "search.json"
	AuthFile     = "search-auth.json"
)

func SettingsPath(dir string) string { return filepath.Join(dir, SettingsFile) }
func AuthPath(dir string) string     { return filepath.Join(dir, AuthFile) }

func LoadSettings(dir string) (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), nil
	}
	return NormalizeSettings(s), nil
}

func LoadAuth(dir string) (Auth, error) {
	a := DefaultAuth()
	data, err := os.ReadFile(AuthPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a, nil
		}
		return a, err
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return DefaultAuth(), nil
	}
	return NormalizeAuth(a), nil
}

func writeAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), config.DirMode); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(path), config.DirMode)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, append(data, '\n'), config.FileMode); err != nil {
		return err
	}
	_ = os.Chmod(tmp, config.FileMode)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path, config.FileMode)
	return nil
}

func SaveSettings(dir string, s Settings) error {
	return writeAtomic(SettingsPath(dir), NormalizeSettings(s))
}

func SaveAuth(dir string, a Auth) error {
	return writeAtomic(AuthPath(dir), NormalizeAuth(a))
}

func Load(dir string) (Settings, Auth, error) {
	s, err := LoadSettings(dir)
	if err != nil {
		return Settings{}, Auth{}, err
	}
	a, err := LoadAuth(dir)
	if err != nil {
		return Settings{}, Auth{}, err
	}
	return s, a, nil
}

func HasAvailable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	s, a, err := Load(dir)
	return err == nil && len(AvailableOrder(s, a)) > 0
}

func SaveAPIKey(dir string, id ProviderID, key string) error {
	if !ValidProvider(id) {
		return fmt.Errorf("motor de búsqueda inválido: %s", id)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("la API key no puede estar vacía")
	}
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	a.APIKeys[id] = key
	ps := s.Providers[id]
	on := true
	ps.Enabled = &on
	// Una credencial nueva nunca hereda la validación de una credencial anterior.
	ps.LastTest = nil
	s.Providers[id] = ps
	// Invalida primero la prueba persistida. Si la escritura del secreto falla,
	// el motor queda de forma segura como no disponible en vez de permitir que
	// una credencial nueva herede accidentalmente la validación anterior.
	if err := SaveSettings(dir, s); err != nil {
		return err
	}
	return SaveAuth(dir, a)
}

func RemoveAPIKey(dir string, id ProviderID) error {
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	delete(a.APIKeys, id)
	ps := s.Providers[id]
	off := false
	ps.Enabled = &off
	ps.LastTest = nil
	s.Providers[id] = ps
	if s.DefaultProvider == id {
		s.DefaultProvider = ""
	}
	if err := SaveAuth(dir, a); err != nil {
		return err
	}
	reassignDefault(&s, a)
	return SaveSettings(dir, s)
}

func SetEnabled(dir string, id ProviderID, enabled bool) error {
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	state := Resolve(id, s, a)
	if enabled {
		if !state.Configured {
			return errors.New("configura una API key antes de habilitar el motor")
		}
		if !state.Validated {
			return errors.New("prueba la conexión correctamente antes de habilitar el motor")
		}
	}
	ps := s.Providers[id]
	ps.Enabled = &enabled
	s.Providers[id] = ps
	if !enabled && s.DefaultProvider == id {
		s.DefaultProvider = ""
	}
	reassignDefault(&s, a)
	return SaveSettings(dir, s)
}

func RecordTest(dir string, id ProviderID, ok bool, message string) error {
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	ps := s.Providers[id]
	ps.LastTest = &TestStatus{OK: ok, Message: strings.TrimSpace(message), TestedAt: time.Now().UTC()}
	if ok && ps.Enabled == nil {
		on := true
		ps.Enabled = &on
	}
	s.Providers[id] = ps
	if !ok && s.DefaultProvider == id {
		s.DefaultProvider = ""
	}
	reassignDefault(&s, a)
	return SaveSettings(dir, s)
}

func SetDefault(dir string, id ProviderID) error {
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	if !Resolve(id, s, a).Available {
		return errors.New("el motor predeterminado debe tener una API key validada y estar habilitado")
	}
	s.DefaultProvider = id
	s.FallbackOrder = NormalizeOrder(append([]ProviderID{id}, s.FallbackOrder...))
	return SaveSettings(dir, s)
}

func SetFallbackOrder(dir string, order []ProviderID) error {
	s, a, err := Load(dir)
	if err != nil {
		return err
	}
	available := map[ProviderID]bool{}
	for _, id := range AvailableOrder(s, a) {
		available[id] = true
	}
	filtered := make([]ProviderID, 0, len(order))
	for _, id := range order {
		if available[id] {
			filtered = append(filtered, id)
		}
	}
	for _, id := range ProviderIDs {
		if available[id] {
			filtered = append(filtered, id)
		}
	}
	s.FallbackOrder = NormalizeOrder(filtered)
	reassignDefault(&s, a)
	return SaveSettings(dir, s)
}

func reassignDefault(s *Settings, a Auth) {
	if s.DefaultProvider != "" && Resolve(s.DefaultProvider, *s, a).Available {
		return
	}
	s.DefaultProvider = ""
	for _, id := range NormalizeOrder(s.FallbackOrder) {
		if Resolve(id, *s, a).Available {
			s.DefaultProvider = id
			return
		}
	}
}
