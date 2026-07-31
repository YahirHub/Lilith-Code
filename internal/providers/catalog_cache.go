package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lilith/li/internal/config"
)

const CatalogCacheFileName = "provider-model-cache.json"

type catalogCache struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updatedAt"`
	Providers map[string][]Model `json:"providers"`
}

func catalogCachePath(dir string) string { return filepath.Join(dir, CatalogCacheFileName) }

func loadCatalogCache(dir string) (catalogCache, error) {
	cache := catalogCache{Version: 1, Providers: map[string][]Model{}}
	data, err := os.ReadFile(catalogCachePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cache, nil
		}
		return cache, err
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		// The cache is disposable. Ignore corruption and rebuild from the bundled
		// fallback catalogs instead of preventing Lilith from starting.
		return catalogCache{Version: 1, Providers: map[string][]Model{}}, nil
	}
	if cache.Providers == nil {
		cache.Providers = map[string][]Model{}
	}
	return cache, nil
}

func applyCatalogCache(dir string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	cache, err := loadCatalogCache(dir)
	if err != nil {
		return err
	}
	for index := range cfg.Providers {
		models := cache.Providers[cfg.Providers[index].ID]
		if len(models) == 0 {
			continue
		}
		cfg.Providers[index].Models = cloneModels(models)
	}
	return nil
}

func saveCatalogCache(dir string, cfg Config) error {
	cache, err := loadCatalogCache(dir)
	if err != nil {
		return err
	}
	for _, provider := range cfg.Providers {
		if !provider.Bundled || len(provider.Models) == 0 {
			continue
		}
		cache.Providers[provider.ID] = cloneModels(provider.Models)
	}
	cache.Version = 1
	cache.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(dir, config.DirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	path := catalogCachePath(dir)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), config.FileMode); err != nil {
		return err
	}
	_ = os.Chmod(tmp, config.FileMode)
	return os.Rename(tmp, path)
}
