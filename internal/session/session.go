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
	"sync"
	"time"

	compactctx "github.com/lilith/li/internal/compaction"
	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	litodo "github.com/lilith/li/internal/todo"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
	// maxTitle acota el título derivado del primer mensaje del usuario.
	maxTitle = 72
)

// TranscriptEntry is a UI-safe snapshot of one rendered chat entry. It is
// deliberately separate from Messages: Messages must remain protocol-correct
// for the provider, while Transcript may contain partial reasoning, in-flight
// tool panels and other progress that is valuable to restore after an
// interruption.
type TranscriptEntry struct {
	Kind     string            `json:"kind"`
	Content  string            `json:"content,omitempty"`
	Time     time.Time         `json:"time,omitempty"`
	File     *FileProgress     `json:"file,omitempty"`
	Command  *CommandProgress  `json:"command,omitempty"`
	Thinking *ThinkingProgress `json:"thinking,omitempty"`
	Agent    *AgentProgress    `json:"agent,omitempty"`
}

type TextEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type FileProgress struct {
	Tool       string     `json:"tool,omitempty"`
	CallID     string     `json:"callId,omitempty"`
	Index      int        `json:"index,omitempty"`
	Path       string     `json:"path,omitempty"`
	Content    string     `json:"content,omitempty"`
	Old        string     `json:"old,omitempty"`
	New        string     `json:"new,omitempty"`
	Edits      []TextEdit `json:"edits,omitempty"`
	Done       bool       `json:"done,omitempty"`
	Failed     bool       `json:"failed,omitempty"`
	Skipped    bool       `json:"skipped,omitempty"`
	Canceled   bool       `json:"canceled,omitempty"`
	Superseded bool       `json:"superseded,omitempty"`
	Result     string     `json:"result,omitempty"`
	Expanded   bool       `json:"expanded,omitempty"`
}

type CommandProgress struct {
	CallID     string        `json:"callId,omitempty"`
	Index      int           `json:"index,omitempty"`
	Command    string        `json:"command,omitempty"`
	Timeout    int           `json:"timeout,omitempty"`
	Done       bool          `json:"done,omitempty"`
	Failed     bool          `json:"failed,omitempty"`
	Superseded bool          `json:"superseded,omitempty"`
	ExitCode   int           `json:"exitCode,omitempty"`
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	TimedOut   bool          `json:"timedOut,omitempty"`
	Canceled   bool          `json:"canceled,omitempty"`
	StartedAt  time.Time     `json:"startedAt,omitempty"`
	Elapsed    time.Duration `json:"elapsed,omitempty"`
	Expanded   bool          `json:"expanded,omitempty"`
}

type ThinkingProgress struct {
	Content  string `json:"content,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Expanded bool   `json:"expanded,omitempty"`
}

// AgentProgress persists the parent-visible projection of one subagent run.
// The full isolated protocol transcript lives in projects/.../subagents; this
// snapshot only restores the live/finished orchestration panel in the parent.
type AgentProgress struct {
	TaskID       string                  `json:"taskId,omitempty"`
	ParentTaskID string                  `json:"parentTaskId,omitempty"`
	Name         string                  `json:"name,omitempty"`
	Description  string                  `json:"description,omitempty"`
	Model        string                  `json:"model,omitempty"`
	Depth        int                     `json:"depth,omitempty"`
	Resumed      bool                    `json:"resumed,omitempty"`
	Background   bool                    `json:"background,omitempty"`
	Status       string                  `json:"status,omitempty"`
	StartedAt    time.Time               `json:"startedAt,omitempty"`
	FinishedAt   time.Time               `json:"finishedAt,omitempty"`
	Reasoning    string                  `json:"reasoning,omitempty"`
	Output       string                  `json:"output,omitempty"`
	Activities   []AgentActivityProgress `json:"activities,omitempty"`
	Expanded     bool                    `json:"expanded,omitempty"`
}

type AgentActivityProgress struct {
	CallID   string    `json:"callId,omitempty"`
	Name     string    `json:"name,omitempty"`
	Args     string    `json:"args,omitempty"`
	Result   string    `json:"result,omitempty"`
	Running  bool      `json:"running,omitempty"`
	Failed   bool      `json:"failed,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
}

// LiveCheckpoint is written to a tiny sidecar while a turn is still mutable.
// It contains only entries added after the last stable session snapshot, so
// streaming persistence does not rewrite the entire historical conversation
// on every token.
type LiveCheckpoint struct {
	Revision            uint64            `json:"revision"`
	BaseTranscriptCount int               `json:"baseTranscriptCount"`
	BaseHistoryCount    int               `json:"baseHistoryCount"`
	UpdatedAt           time.Time         `json:"updatedAt"`
	Entries             []TranscriptEntry `json:"entries,omitempty"`
	History             []openai.Message  `json:"history,omitempty"`
	Todo                *litodo.State     `json:"todo,omitempty"`
	Plan                *planstate.State  `json:"plan,omitempty"`
	Goal                *ligoal.State     `json:"goal,omitempty"`
}

// CompactionRecord preserves the exact provider history removed from the active
// context. The transcript stays visually complete, while this archive makes
// compaction reversible/auditable instead of deleting the original protocol
// messages from the session file.
type CompactionRecord struct {
	ID               string           `json:"id"`
	CreatedAt        time.Time        `json:"createdAt"`
	Auto             bool             `json:"auto"`
	Instructions     string           `json:"instructions,omitempty"`
	Summary          string           `json:"summary"`
	TokensBefore     int              `json:"tokensBefore"`
	TokensAfter      int              `json:"tokensAfter"`
	SplitTurn        bool             `json:"splitTurn,omitempty"`
	FullCompaction   bool             `json:"fullCompaction,omitempty"`
	ArchivedMessages []openai.Message `json:"archivedMessages,omitempty"`
}

// ForkOrigin records the parent conversation and workspace used to create an
// independent /fork session.
type ForkOrigin struct {
	SessionID   string    `json:"sessionId"`
	ProjectPath string    `json:"projectPath"`
	PointID     string    `json:"pointId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Session is one persisted conversation.
type Session struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	ProjectPath string             `json:"projectPath"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
	Messages    []openai.Message   `json:"messages"`
	Transcript  []TranscriptEntry  `json:"transcript,omitempty"`
	Compactions []CompactionRecord `json:"compactions,omitempty"`
	Revision    uint64             `json:"revision,omitempty"`
	Todo        *litodo.State      `json:"todo,omitempty"`
	Plan        *planstate.State   `json:"plan,omitempty"`
	Goal        *ligoal.State      `json:"goal,omitempty"`
	Live        *LiveCheckpoint    `json:"-"`
	ForkedFrom  *ForkOrigin        `json:"forkedFrom,omitempty"`
}

// Meta is the lightweight row shown in `/history`.
type Meta struct {
	ID        string
	Title     string
	UpdatedAt time.Time
	Turns     int
}

// Store owns the on-disk layout: <configDir>/projects/<slug>/chats/<id>.json
type Store struct {
	root         string
	mu           sync.Mutex
	liveRevision map[string]uint64
}

// NewStore builds a store rooted at the Lilith config directory.
func NewStore(configDir string) *Store {
	return &Store{root: filepath.Join(configDir, "projects"), liveRevision: map[string]uint64{}}
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

// Clone returns a detached deep copy suitable for rewind checkpoints and
// session forks. Live sidecars are intentionally excluded because they may
// represent an in-flight turn rather than a stable protocol state.
func Clone(c *Session) *Session {
	if c == nil {
		return nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	var out Session
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	out.Live = nil
	return &out
}

// Fork creates an independent session with the same conversation state but a
// new ID/project path. Rewind history is deliberately not copied because it is
// stored outside Session and belongs to the source timeline.
func Fork(c *Session, projectPath, title, pointID string) *Session {
	out := Clone(c)
	if out == nil {
		out = New(projectPath)
	}
	now := time.Now()
	sourceID := out.ID
	sourceProject := out.ProjectPath
	out.ID = now.Format("20060102-150405") + "-" + randomSuffix()
	out.ProjectPath = filepath.Clean(projectPath)
	out.CreatedAt = now
	out.UpdatedAt = now
	out.Revision = 0
	out.Live = nil
	if strings.TrimSpace(title) != "" {
		out.Title = strings.TrimSpace(title)
	} else if strings.TrimSpace(out.Title) == "" || out.Title == "Sin título" {
		out.Title = "Fork de " + sourceID
	} else {
		out.Title = out.Title + " (fork)"
	}
	out.ForkedFrom = &ForkOrigin{SessionID: sourceID, ProjectPath: sourceProject, PointID: pointID, CreatedAt: now}
	return out
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

func firstUserTitle(messages []openai.Message) string {
	for _, message := range messages {
		if message.Role != "user" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		if _, summary := compactctx.SummaryFromMessage(message); summary {
			continue
		}
		title := strings.TrimSpace(strings.ReplaceAll(message.Content, "\n", " "))
		if len([]rune(title)) > maxTitle {
			title = string([]rune(title)[:maxTitle]) + "…"
		}
		return title
	}
	return ""
}

// Touch refreshes the title (from the first real user message) and timestamp.
// A full-context compaction may leave only a summary in Messages, so archives
// are consulted as a fallback instead of turning a valid session anonymous.
func (c *Session) Touch() {
	c.UpdatedAt = time.Now()
	if c.Title != "" && c.Title != "Sin título" {
		return
	}
	if title := firstUserTitle(c.Messages); title != "" {
		c.Title = title
		return
	}
	for _, record := range c.Compactions {
		if title := firstUserTitle(record.ArchivedMessages); title != "" {
			c.Title = title
			return
		}
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

// SaveLive writes only the mutable tail of an active turn. Keeping this in a
// sidecar avoids serializing a potentially huge completed chat every few
// hundred milliseconds while the provider is streaming tokens.
func (s *Store) SaveLive(projectPath, id string, live *LiveCheckpoint) error {
	if live == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return err
	}
	data, err := json.Marshal(live)
	if err != nil {
		return err
	}
	final := filepath.Join(dir, id+".live")
	s.mu.Lock()
	defer s.mu.Unlock()
	if floor := s.liveRevision[final]; live.Revision <= floor {
		return nil
	}
	// Revision-specific temp names allow an in-flight older write and a forced
	// Ctrl+C checkpoint to coexist without sharing the same temporary file.
	tmp := fmt.Sprintf("%s.%d.tmp", final, live.Revision)
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.liveRevision[final] = live.Revision
	return nil
}

// ClearLive removes the mutable checkpoint after a stable snapshot has been
// committed. A missing sidecar is already the desired state.
func (s *Store) ClearLive(projectPath, id string, revision uint64) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return err
	}
	final := filepath.Join(dir, id+".live")
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision > s.liveRevision[final] {
		s.liveRevision[final] = revision
	}
	err = os.Remove(final)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Store) loadLive(projectPath, id string) (*LiveCheckpoint, error) {
	dir, err := s.ChatsDir(projectPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".live"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var live LiveCheckpoint
	if err := json.Unmarshal(data, &live); err != nil {
		return nil, err
	}
	return &live, nil
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
	// A live sidecar is valid only when it is newer than the last stable
	// snapshot. This revision check also makes a late asynchronous write from an
	// older checkpoint harmless after Save + ClearLive.
	if live, liveErr := s.loadLive(projectPath, id); liveErr == nil && live != nil && live.Revision > c.Revision {
		c.Live = live
	}
	return &c, nil
}

func countUserTurns(messages []openai.Message) int {
	turns := 0
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if _, summary := compactctx.SummaryFromMessage(message); summary {
			continue
		}
		turns++
	}
	return turns
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
		turns := countUserTurns(c.Messages)
		for _, record := range c.Compactions {
			turns += countUserTurns(record.ArchivedMessages)
		}
		if c.Live != nil {
			turns += countUserTurns(c.Live.History)
		}
		updatedAt := c.UpdatedAt
		if c.Live != nil && c.Live.UpdatedAt.After(updatedAt) {
			updatedAt = c.Live.UpdatedAt
		}
		out = append(out, Meta{ID: c.ID, Title: c.Title, UpdatedAt: updatedAt, Turns: turns})
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
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, id+".live"))
	return nil
}
