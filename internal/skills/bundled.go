package skills

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

const bundledSkillsCacheDir = "bundled-skills"

var bundledDirCache sync.Map

// BundledDir materializes the read-only assets/skills filesystem into a
// content-addressed private cache and returns the directory that the normal
// filesystem-based skill runtime can scan. The extraction happens at most once
// per config directory for the lifetime of the process.
//
// Materializing keeps one implementation for skill_read/skill_search/
// skill_files: bundled and user/project skills go through the same path
// validation, resource classification and bounded-reading code.
func BundledDir(configDir string) string {
	if configDir == "" {
		return ""
	}
	key := filepath.Clean(configDir)
	if cached, ok := bundledDirCache.Load(key); ok {
		return cached.(string)
	}
	dir, err := materializeBundledSkills(key)
	if err != nil {
		// Skill discovery is deliberately best-effort. A damaged cache or a
		// read-only config directory must not prevent Lilith from starting. Do
		// not cache the failure so a later load in the same process can retry.
		return ""
	}
	actual, _ := bundledDirCache.LoadOrStore(key, dir)
	return actual.(string)
}

func materializeBundledSkills(configDir string) (string, error) {
	source := lilithassets.SkillsFS()
	digest, err := hashEmbeddedTree(source)
	if err != nil {
		return "", err
	}
	cacheRoot := filepath.Join(configDir, ".cache", bundledSkillsCacheDir)
	target := filepath.Join(cacheRoot, digest[:16])
	ready := filepath.Join(target, ".ready")
	if info, err := os.Stat(ready); err == nil && !info.IsDir() {
		return target, nil
	}

	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return "", fmt.Errorf("create bundled skill cache: %w", err)
	}
	_ = os.Chmod(cacheRoot, 0o700)

	tmp, err := os.MkdirTemp(cacheRoot, ".extract-")
	if err != nil {
		return "", fmt.Errorf("create bundled skill staging dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	_ = os.Chmod(tmp, 0o700)

	if err := copyEmbeddedTree(source, tmp); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ready"), []byte(digest+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write bundled skill marker: %w", err)
	}

	// Another Lilith process may have completed the same extraction while this
	// one was staging files. Prefer the already-complete content-addressed copy.
	if info, err := os.Stat(ready); err == nil && !info.IsDir() {
		return target, nil
	}
	if err := os.Rename(tmp, target); err != nil {
		// Rename is atomic and the .ready marker already lives inside tmp. If a
		// racing process published the same hash first, just use its completed
		// directory; never delete another running process's cache.
		if info, statErr := os.Stat(ready); statErr == nil && !info.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("publish bundled skill cache: %w", err)
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
		return "", fmt.Errorf("hash embedded skills: %w", err)
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
			if err := os.MkdirAll(out, 0o700); err != nil {
				return fmt.Errorf("create bundled skill directory %s: %w", path, err)
			}
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return fmt.Errorf("read embedded skill resource %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return fmt.Errorf("create bundled skill parent %s: %w", path, err)
		}
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return fmt.Errorf("write bundled skill resource %s: %w", path, err)
		}
		return nil
	})
}
