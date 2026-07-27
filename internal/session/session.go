// Package session persists chat conversations per project so Lilith can
// resume them later (`li --continue` o `/history`), igual que Codewolf.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lilith/li/internal/providers/openai"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
	// maxTitle acota el título derivado del primer mensaje del usuario.
	maxTitle = 72
)

// Session is one persisted conversation.
type Session struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	ProjectPath string           `json:"projectPath"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	Messages    []openai.Message `json:"messages"`
}

// Meta is the lightweight row shown in `/history`.
type Meta struct {
	ID        string
	Title     string
	UpdatedAt time.Time
	Turns     int
}

// Store owns the on-disk layout: <configDir>/projects/<slug>/chats/<id>.json
type Store struct{ root string }

// NewStore builds a store rooted at the Lilith config directory.
func NewStore(configDir string) *Store {
	return &Store{root: filepath.Join(configDir, "projects")}
}

// slug derives a stable, collision-free directory name for a project path:
// carpeta legible + hash corto de la ruta completa (dos proyectos con el
// mismo nombre base no comparten historial).
func slug(projectPath string) string {
	clean := filepath.Clean(projectPath)
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.ToSlash(clean))))
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "root"
	}
	safe := make([]rune, 0, len(base))
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			safe = append(safe, r)
		default:
			safe = append(safe, '-')
		}
	}
	return string(safe) + "-" + hex.EncodeToString(sum[:4])
}

// ChatsDir returns (and creates) the chats directory of a project.
func (s *Store) ChatsDir(projectPath string) (string, error) {
	d := filepath.Join(s.root, slug(projectPath), "chats")
	if err := os.MkdirAll(d, dirMode); err != nil {
		return "", err
	}
	return d, nil
}

// New starts an empty in-memory session for a project.
func New(projectPath string) *Session {
	now := time.Now()
	return &Session{
		ID:          now.Format("20060102-150405") + "-" + randomSuffix(),
		Title:       "Sin título",
		ProjectPath: filepath.Clean(projectPath),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func randomSuffix() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * i))
		}
	}
	return hex.EncodeToString(b[:])
}

// Touch refreshes the title (from the first user message) and timestamp.
func (c *Session) Touch() {
	c.UpdatedAt = time.Now()
	if c.Title != "" && c.Title != "Sin título" {
		return
	}
	for _, m := range c.Messages {
		if m.Role != "user" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		t := strings.TrimSpace(strings.ReplaceAll(m.Content, "\n", " "))
		if len([]rune(t)) > maxTitle {
			t = string([]rune(t)[:maxTitle]) + "…"
		}
		c.Title = t
		return
	}
}

// Save writes the session atomically. Empty conversations are skipped so the
// history does not fill with blank entries.
func (s *Store) Save(c *Session) error {
	if c == nil || len(c.Messages) == 0 {
		return nil
	}
	c.Touch()
	dir, err := s.ChatsDir(c.ProjectPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(dir, c.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Load reads one session by ID.
func (s *Store) Load(projectPath, id string) (*Session, error) {
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, err
	}
	var c Session
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("sesión corrupta %s: %w", id, err)
	}
	if c.ProjectPath == "" {
		c.ProjectPath = filepath.Clean(projectPath)
	}
	return &c, nil
}

// List returns the sessions of a project, most recently updated first.
func (s *Store) List(projectPath string) ([]Meta, error) {
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		c, err := s.Load(projectPath, id)
		if err != nil {
			continue
		}
		turns := 0
		for _, m := range c.Messages {
			if m.Role == "user" {
				turns++
			}
		}
		out = append(out, Meta{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt, Turns: turns})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Latest returns the most recently updated session, or nil when there is none.
func (s *Store) Latest(projectPath string) (*Session, error) {
	metas, err := s.List(projectPath)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, nil
	}
	return s.Load(projectPath, metas[0].ID)
}

// Delete removes one session file.
func (s *Store) Delete(projectPath, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("id vacío")
	}
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, id+".json"))
}
