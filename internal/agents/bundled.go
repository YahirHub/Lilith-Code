package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	lilithassets "github.com/lilith/li/assets"
)

const bundledAgentsCacheDir = "bundled-agents"

var bundledDirCache sync.Map

// BundledDir materializes embedded agent Markdown into Lilith's private cache
// so bundled/custom definitions share one filesystem parser and precedence path.
func BundledDir(configDir string) string {
	if configDir == "" {
		return ""
	}
	key := filepath.Clean(configDir)
	if cached, ok := bundledDirCache.Load(key); ok {
		return cached.(string)
	}
	dir, err := materializeBundledAgents(key)
	if err != nil {
		return ""
	}
	actual, _ := bundledDirCache.LoadOrStore(key, dir)
	return actual.(string)
}

func materializeBundledAgents(configDir string) (string, error) {
	source := lilithassets.AgentsFS()
	digest, err := hashEmbeddedTree(source)
	if err != nil {
		return "", err
	}
	cacheRoot := filepath.Join(configDir, ".cache", bundledAgentsCacheDir)
	target := filepath.Join(cacheRoot, digest[:16])
	ready := filepath.Join(target, ".ready")
	if info, err := os.Stat(ready); err == nil && !info.IsDir() {
		return target, nil
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(cacheRoot, 0o700)
	tmp, err := os.MkdirTemp(cacheRoot, ".extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	_ = os.Chmod(tmp, 0o700)
	if err := copyEmbeddedTree(source, tmp); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ready"), []byte(digest+"\n"), 0o600); err != nil {
		return "", err
	}
	if info, err := os.Stat(ready); err == nil && !info.IsDir() {
		return target, nil
	}
	if err := os.Rename(tmp, target); err != nil {
		if info, statErr := os.Stat(ready); statErr == nil && !info.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("publish bundled agent cache: %w", err)
	}
	return target, nil
}

func hashEmbeddedTree(root fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyEmbeddedTree(root fs.FS, dst string) error {
	return fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		out := filepath.Join(dst, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o600)
	})
}
