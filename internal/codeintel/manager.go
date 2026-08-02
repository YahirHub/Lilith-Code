package codeintel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxIndexedFileBytes = 1 << 20
	maxIndexedFiles     = 25000
)

// Manager owns one persistent repository index.
type Manager struct {
	root      string
	configDir string
	cachePath string

	mu      sync.Mutex
	profile Profile
	index   Index
	loaded  bool
}

// New creates a manager without scanning the repository. Detection is cheap and
// available immediately; syntax indexing remains lazy until a code-intel tool is used.
func New(root, configDir string) *Manager {
	if strings.TrimSpace(root) == "" {
		root, _ = os.Getwd()
	}
	root, _ = filepath.Abs(root)
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(resolved)
	}
	if strings.TrimSpace(configDir) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, ".li")
		}
	}
	hashRoot := filepath.Clean(root)
	if runtime.GOOS == "windows" {
		hashRoot = strings.ToLower(hashRoot)
	}
	h := sha256.Sum256([]byte(hashRoot))
	cachePath := filepath.Join(configDir, "codeintel", hex.EncodeToString(h[:12]), "index.json")
	m := &Manager{root: root, configDir: configDir, cachePath: cachePath}
	m.profile = detectProfile(root)
	m.index = Index{Version: indexVersion, Root: root, Files: map[string]FileRecord{}}
	return m
}

func detectProfile(root string) Profile {
	profile := Profile{Environment: detectEnvironment(), Project: detectProject(root)}
	profile.Adapters = availableAdapterNamesFor(profile, root)
	profile.LSPServers = availableLSPServers(profile.Environment)
	profile.SCIPIndex = findSCIPIndex(root)
	return profile
}

// Root returns the canonical project root.
func (m *Manager) Root() string { return m.root }

// Profile returns a copy of the current host/project detection.
func (m *Manager) Profile() Profile {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneProfile(m.profile)
}

// RefreshProfile repeats inexpensive detection. It is useful after installing a
// language server or adding a project manifest during the same session.
func (m *Manager) RefreshProfile() Profile {
	profile := detectProfile(m.root)
	m.mu.Lock()
	m.profile = profile
	m.mu.Unlock()
	return cloneProfile(profile)
}

func cloneProfile(p Profile) Profile {
	out := p
	out.Environment.Path = append([]string(nil), p.Environment.Path...)
	out.Environment.PackageTools = append([]string(nil), p.Environment.PackageTools...)
	out.Environment.Tools = cloneStringMap(p.Environment.Tools)
	out.Project.Kinds = append([]string(nil), p.Project.Kinds...)
	out.Project.Frameworks = append([]string(nil), p.Project.Frameworks...)
	out.Project.Manifests = append([]string(nil), p.Project.Manifests...)
	out.Project.Languages = cloneIntMap(p.Project.Languages)
	out.Adapters = append([]string(nil), p.Adapters...)
	out.LSPServers = append([]string(nil), p.LSPServers...)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *Manager) loadLocked() {
	if m.loaded {
		return
	}
	m.loaded = true
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		return
	}
	var idx Index
	if json.Unmarshal(data, &idx) != nil || idx.Version != indexVersion || filepath.Clean(idx.Root) != m.root {
		return
	}
	if idx.Files == nil {
		idx.Files = map[string]FileRecord{}
	}
	for path := range idx.Files {
		if !safeIndexedPath(path) {
			delete(idx.Files, path)
		}
	}
	m.index = idx
}

func safeIndexedPath(path string) bool {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func (m *Manager) saveLocked() error {
	m.index.Version = indexVersion
	m.index.Root = m.root
	m.index.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(m.index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0o700); err != nil {
		return err
	}
	tmp := m.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := replaceFile(tmp, m.cachePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func replaceFile(source, target string) error {
	if runtime.GOOS == "windows" {
		// Windows does not replace an existing destination atomically with
		// os.Rename. The index is a disposable cache, so removing the previous
		// snapshot before the final rename is safe and avoids stale .tmp files.
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(source, target)
}

// EnsureFresh incrementally parses only added or modified source files and
// removes deleted records. It is safe to call from concurrent tools.
func (m *Manager) EnsureFresh(ctx context.Context) (IndexStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()

	// Build the next snapshot separately. A cancellation or traversal failure
	// must never partially mutate the live index or delete files that were not
	// visited yet.
	nextFiles := cloneFileRecords(m.index.Files)
	seen := map[string]bool{}
	stats := IndexStats{}
	complete := true
	walkErr := filepath.WalkDir(m.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.Type()&os.ModeSymlink != 0 {
			stats.Skipped++
			return nil
		}
		if d.IsDir() {
			if path != m.root && shouldIgnoreDirectory(m.root, path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if stats.Files+stats.Skipped >= maxIndexedFiles {
			complete = false
			return filepath.SkipAll
		}
		lang := languageForPath(path)
		if lang == "" {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || info.Size() > maxIndexedFileBytes || info.Size() < 0 {
			stats.Skipped++
			return nil
		}
		rel, relErr := filepath.Rel(m.root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		stats.Files++
		old, exists := nextFiles[rel]
		if exists && old.Size == info.Size() && old.ModUnixNano == info.ModTime().UnixNano() && old.Language == lang {
			return nil
		}
		record, parseErr := parseFile(m.root, path, rel, lang, info)
		if parseErr != nil {
			record.ParseError = parseErr.Error()
		}
		nextFiles[rel] = record
		stats.Parsed++
		return nil
	})
	if walkErr != nil {
		return stats, walkErr
	}
	if complete {
		for path := range nextFiles {
			if !seen[path] {
				delete(nextFiles, path)
				stats.Removed++
			}
		}
	}
	for _, record := range nextFiles {
		stats.Symbols += len(record.Symbols)
		stats.References += len(record.References)
	}
	m.index.Files = nextFiles
	if err := m.saveLocked(); err != nil {
		return stats, fmt.Errorf("guardar índice de código: %w", err)
	}
	stats.UpdatedAt = m.index.UpdatedAt
	return stats, nil
}

func cloneFileRecords(in map[string]FileRecord) map[string]FileRecord {
	out := make(map[string]FileRecord, len(in))
	for path, record := range in {
		record.Symbols = append([]Symbol(nil), record.Symbols...)
		record.References = append([]Reference(nil), record.References...)
		record.Imports = append([]string(nil), record.Imports...)
		record.ImportAliases = cloneStringMap(record.ImportAliases)
		out[path] = record
	}
	return out
}

// Snapshot returns a deep copy after loading the persistent index.
func (m *Manager) Snapshot() Index {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	out := m.index
	out.Files = make(map[string]FileRecord, len(m.index.Files))
	for path, record := range m.index.Files {
		record.Symbols = append([]Symbol(nil), record.Symbols...)
		record.References = append([]Reference(nil), record.References...)
		record.Imports = append([]string(nil), record.Imports...)
		record.ImportAliases = cloneStringMap(record.ImportAliases)
		out.Files[path] = record
	}
	return out
}

// PromptBlock is deliberately compact and never triggers a full index scan.
func (m *Manager) PromptBlock() string {
	p := m.Profile()
	var b strings.Builder
	b.WriteString("Code intelligence profile: ")
	if p.Environment.Termux {
		fmt.Fprintf(&b, "termux/%s", p.Environment.Arch)
	} else {
		fmt.Fprintf(&b, "%s/%s", p.Environment.OS, p.Environment.Arch)
	}
	if p.Environment.Distribution != "" && p.Environment.Distribution != p.Environment.OS && !p.Environment.Termux {
		fmt.Fprintf(&b, ", distro=%s", p.Environment.Distribution)
	}
	if p.Environment.WSL {
		b.WriteString(", wsl=true")
	}
	if p.Environment.SSH {
		b.WriteString(", ssh=true")
	}
	if p.Environment.Container {
		b.WriteString(", container=true")
	}
	if p.Environment.Shell != "" {
		fmt.Fprintf(&b, ", shell=%s", p.Environment.Shell)
	}
	if len(p.Project.Kinds) > 0 {
		fmt.Fprintf(&b, ", project=%s", strings.Join(p.Project.Kinds, "+"))
	}
	if p.Project.PrimaryLanguage != "" {
		fmt.Fprintf(&b, ", primary=%s", p.Project.PrimaryLanguage)
	}
	if len(p.Project.Frameworks) > 0 {
		fmt.Fprintf(&b, ", frameworks=%s", strings.Join(p.Project.Frameworks, ","))
	}
	if p.Project.PackageManager != "" {
		fmt.Fprintf(&b, ", package_manager=%s", p.Project.PackageManager)
	}
	if len(p.Adapters) > 0 {
		fmt.Fprintf(&b, ", adapters=%s", strings.Join(p.Adapters, ","))
	}
	if len(p.LSPServers) > 0 {
		fmt.Fprintf(&b, ", lsp=%s", strings.Join(p.LSPServers, ","))
	}
	if p.SCIPIndex != "" {
		b.WriteString(", scip=index.scip")
	}
	b.WriteString(". Respect the detected OS, shell and package manager; never assume sudo, bash or POSIX paths on Windows or Termux. Prefer code_context/code_symbols/code_references before broad file reads; use code_semantic when an LSP server is available and code_validate after edits.")
	return b.String()
}

// StatusText is a user/model-readable summary.
func (m *Manager) StatusText(ctx context.Context, refresh bool) (string, error) {
	profile := m.RefreshProfile()
	var stats IndexStats
	var err error
	if refresh {
		stats, err = m.EnsureFresh(ctx)
	} else {
		idx := m.Snapshot()
		stats.Files = len(idx.Files)
		stats.UpdatedAt = idx.UpdatedAt
		for _, file := range idx.Files {
			stats.Symbols += len(file.Symbols)
			stats.References += len(file.References)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "root: %s\nhost: %s/%s", profile.Project.Root, profile.Environment.OS, profile.Environment.Arch)
	if profile.Environment.Termux {
		b.WriteString(" (Termux)")
	}
	fmt.Fprintf(&b, "\nproject: %s\nprimary_language: %s\npackage_manager: %s", emptyAs(strings.Join(profile.Project.Kinds, ", "), "unknown"), emptyAs(profile.Project.PrimaryLanguage, "unknown"), emptyAs(profile.Project.PackageManager, "none"))
	fmt.Fprintf(&b, "\nframeworks: %s", emptyAs(strings.Join(profile.Project.Frameworks, ", "), "none detected"))
	fmt.Fprintf(&b, "\nmanifests: %s", emptyAs(strings.Join(profile.Project.Manifests, ", "), "none"))
	fmt.Fprintf(&b, "\nindex_path: %s", m.cachePath)
	fmt.Fprintf(&b, "\nindex: %d files, %d symbols, %d references", stats.Files, stats.Symbols, stats.References)
	if !stats.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, " (updated %s)", stats.UpdatedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "\nadapters: %s", emptyAs(strings.Join(profile.Adapters, ", "), "none detected"))
	fmt.Fprintf(&b, "\nlsp: %s", emptyAs(strings.Join(profile.LSPServers, ", "), "none detected"))
	fmt.Fprintf(&b, "\nscip: %s", emptyAs(profile.SCIPIndex, "not detected"))
	return b.String(), err
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sortedFileKeys(files map[string]FileRecord) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
