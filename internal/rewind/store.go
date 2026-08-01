package rewind

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/session"
)

const (
	dirMode          = 0o700
	fileMode         = 0o600
	defaultMaxPoints = 80
)

// Store owns rewind data under the Lilith config directory. Points are scoped
// by project and session; fallback blobs are shared within the project so
// repeated checkpoints deduplicate unchanged files.
type Store struct {
	root string
	mu   sync.Mutex
}

func NewStore(configDir string) *Store {
	return &Store{root: filepath.Join(configDir, "rewind")}
}

func lockMutexContext(ctx context.Context, mu *sync.Mutex) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if mu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func projectSlug(projectPath string) string {
	clean := filepath.Clean(projectPath)
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.ToSlash(clean))))
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "root"
	}
	var safe strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			safe.WriteRune(r)
		default:
			safe.WriteByte('-')
		}
	}
	return safe.String() + "-" + hex.EncodeToString(sum[:4])
}

func cleanID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	v := strings.Trim(b.String(), "-.")
	if v == "" {
		return "unknown"
	}
	return v
}

func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * i))
		}
	}
	return hex.EncodeToString(b[:])
}

func newPointID() string {
	return time.Now().UTC().Format("20060102T150405.000000000") + "-" + randomSuffix()
}

func (s *Store) projectDir(projectPath string) string {
	return filepath.Join(s.root, projectSlug(projectPath))
}

func (s *Store) sessionDir(projectPath, sessionID string) string {
	return filepath.Join(s.projectDir(projectPath), "sessions", cleanID(sessionID))
}

func (s *Store) pointPath(projectPath, sessionID, pointID string) string {
	return filepath.Join(s.sessionDir(projectPath, sessionID), cleanID(pointID)+".json.gz")
}

func (s *Store) metaPath(projectPath, sessionID, pointID string) string {
	return filepath.Join(s.sessionDir(projectPath, sessionID), cleanID(pointID)+".meta.json")
}

func (s *Store) blobDir(projectPath string) string {
	return filepath.Join(s.projectDir(projectPath), "blobs")
}

func (s *Store) gitIndexDir(projectPath string) string {
	return filepath.Join(s.projectDir(projectPath), "tmp")
}

// Create records a conversation checkpoint. Workspace capture is intentionally
// lazy and may be attached later by CaptureWorkspace.
func (s *Store) Create(projectPath, sessionID, prompt string, conversation *session.Session) (Meta, error) {
	return s.createContext(context.Background(), projectPath, sessionID, prompt, "turn", conversation)
}

// CreateSafety records an internal recovery point. Safety points are visible in
// /rewind but do not repopulate the input editor when restored.
func (s *Store) CreateSafety(projectPath, sessionID, label string, conversation *session.Session) (Meta, error) {
	return s.CreateSafetyContext(context.Background(), projectPath, sessionID, label, conversation)
}

// CreateSafetyContext creates a cancelable safety point. This matters when a
// previous workspace capture is still releasing the store lock after Escape.
func (s *Store) CreateSafetyContext(ctx context.Context, projectPath, sessionID, label string, conversation *session.Session) (Meta, error) {
	return s.createContext(ctx, projectPath, sessionID, label, "safety", conversation)
}

func (s *Store) createContext(ctx context.Context, projectPath, sessionID, prompt, kind string, conversation *session.Session) (Meta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Meta{}, err
	}
	if conversation == nil {
		return Meta{}, errors.New("checkpoint conversation is nil")
	}
	pointID := newPointID()
	meta := Meta{
		ID:          pointID,
		SessionID:   sessionID,
		ProjectPath: filepath.Clean(projectPath),
		Prompt:      prompt,
		Kind:        strings.TrimSpace(kind),
		CreatedAt:   time.Now(),
	}
	point := &Point{Meta: meta, Conversation: conversation}

	if err := lockMutexContext(ctx, &s.mu); err != nil {
		return Meta{}, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Meta{}, err
	}
	if err := s.writePointLocked(point); err != nil {
		return Meta{}, err
	}
	if err := s.pruneLocked(projectPath, sessionID, defaultMaxPoints); err != nil {
		return meta, err
	}
	return meta, nil
}

func (s *Store) writePointLocked(point *Point) error {
	if point == nil || point.Conversation == nil {
		return errors.New("invalid rewind point")
	}
	dir := s.sessionDir(point.Meta.ProjectPath, point.Meta.SessionID)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	final := s.pointPath(point.Meta.ProjectPath, point.Meta.SessionID, point.Meta.ID)
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)
	writeErr := enc.Encode(point)
	closeGZErr := gz.Close()
	closeFileErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(tmp)
		return writeErr
	}
	if closeGZErr != nil {
		_ = os.Remove(tmp)
		return closeGZErr
	}
	if closeFileErr != nil {
		_ = os.Remove(tmp)
		return closeFileErr
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return writeJSONAtomic(s.metaPath(point.Meta.ProjectPath, point.Meta.SessionID, point.Meta.ID), point.Meta)
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) Load(projectPath, sessionID, pointID string) (*Point, error) {
	path := s.pointPath(projectPath, sessionID, pointID)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var point Point
	if err := json.NewDecoder(gz).Decode(&point); err != nil {
		return nil, err
	}
	if point.Conversation == nil {
		return nil, fmt.Errorf("checkpoint %s has no conversation", pointID)
	}
	return &point, nil
}

func (s *Store) List(projectPath, sessionID string) ([]Meta, error) {
	dir := s.sessionDir(projectPath, sessionID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		var meta Meta
		if json.Unmarshal(data, &meta) != nil || meta.ID == "" {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) Delete(projectPath, sessionID, pointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	point, _ := s.loadUnlocked(projectPath, sessionID, pointID)
	if point != nil && point.Workspace.Kind == workspaceGit && point.Workspace.GitRef != "" {
		_ = deleteGitRef(point.Workspace.Root, point.Workspace.GitRef)
	}
	var errs []error
	for _, path := range []string{
		s.pointPath(projectPath, sessionID, pointID),
		s.metaPath(projectPath, sessionID, pointID),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) loadUnlocked(projectPath, sessionID, pointID string) (*Point, error) {
	path := s.pointPath(projectPath, sessionID, pointID)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var point Point
	if err := json.NewDecoder(gz).Decode(&point); err != nil {
		return nil, err
	}
	return &point, nil
}

func (s *Store) pruneLocked(projectPath, sessionID string, max int) error {
	if max <= 0 {
		return nil
	}
	metas, err := s.List(projectPath, sessionID)
	if err != nil || len(metas) <= max {
		return err
	}
	var errs []error
	for _, meta := range metas[:len(metas)-max] {
		point, _ := s.loadUnlocked(projectPath, sessionID, meta.ID)
		if point != nil && point.Workspace.Kind == workspaceGit && point.Workspace.GitRef != "" {
			_ = deleteGitRef(point.Workspace.Root, point.Workspace.GitRef)
		}
		for _, path := range []string{s.pointPath(projectPath, sessionID, meta.ID), s.metaPath(projectPath, sessionID, meta.ID)} {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				errs = append(errs, removeErr)
			}
		}
	}
	return errors.Join(errs...)
}

// CaptureWorkspace attaches a code snapshot to an existing point. It is safe
// to call repeatedly; once a snapshot exists it is returned unchanged.
func (s *Store) CaptureWorkspace(projectPath, sessionID, pointID string) (Meta, error) {
	return s.CaptureWorkspaceContext(context.Background(), projectPath, sessionID, pointID)
}

// CaptureWorkspaceContext is the cancelable form used by interactive rewind.
// A canceled Git/filter process must release the store lock instead of leaving
// the Rewind screen stuck forever in "Restaurando…".
func (s *Store) CaptureWorkspaceContext(ctx context.Context, projectPath, sessionID, pointID string) (Meta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Meta{}, err
	}
	if err := lockMutexContext(ctx, &s.mu); err != nil {
		return Meta{}, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Meta{}, err
	}
	point, err := s.loadUnlocked(projectPath, sessionID, pointID)
	if err != nil {
		return Meta{}, err
	}
	if point.Meta.HasCode && point.Workspace.Kind != workspaceNone {
		return point.Meta, nil
	}

	workspace, captureErr := s.captureWorkspaceLocked(ctx, projectPath, sessionID, pointID)
	if captureErr != nil {
		point.Meta.CodeError = captureErr.Error()
		if writeErr := s.writePointLocked(point); writeErr != nil {
			return point.Meta, errors.Join(captureErr, writeErr)
		}
		return point.Meta, captureErr
	}
	point.Workspace = workspace
	point.Meta.HasCode = true
	point.Meta.CodeError = ""
	point.Meta.PartialCode = len(workspace.Skipped) > 0
	point.Meta.FileCount = len(workspace.Files)
	for _, entry := range workspace.Files {
		point.Meta.TotalBytes += entry.Size
	}
	if workspace.Kind == workspaceGit {
		if count, countErr := gitFileCountContext(ctx, workspace.Root, workspace.GitCommit, workspace.WorkingRel); countErr == nil {
			point.Meta.FileCount = count
		}
	}
	if err := ctx.Err(); err != nil {
		return point.Meta, err
	}
	if err := s.writePointLocked(point); err != nil {
		return point.Meta, err
	}
	return point.Meta, nil
}

func (s *Store) captureWorkspaceLocked(ctx context.Context, projectPath, sessionID, pointID string) (WorkspaceSnapshot, error) {
	if snapshot, err := captureGitWorkspaceContext(ctx, projectPath, s.gitIndexDir(projectPath), sessionID, pointID); err == nil {
		return snapshot, nil
	} else if ctx.Err() != nil {
		return WorkspaceSnapshot{}, ctx.Err()
	}
	return captureFileWorkspaceContext(ctx, projectPath, s.blobDir(projectPath))
}

// RestoreWorkspace restores only code/files. Conversation restoration is
// handled by the TUI after this operation succeeds.
func (s *Store) RestoreWorkspace(point *Point) error {
	return s.RestoreWorkspaceContext(context.Background(), point)
}

// RestoreWorkspaceContext is cancelable so interactive rewind can be aborted
// with Escape and can terminate a Git filter/process that stops responding.
func (s *Store) RestoreWorkspaceContext(ctx context.Context, point *Point) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if point == nil || !point.Meta.HasCode {
		return errors.New("el checkpoint no contiene un snapshot de código")
	}
	switch point.Workspace.Kind {
	case workspaceGit:
		return restoreGitWorkspaceContext(ctx, point.Workspace)
	case workspaceFiles:
		return restoreFileWorkspaceContext(ctx, point.Workspace, s.blobDir(point.Meta.ProjectPath), point.Meta.ProjectPath)
	default:
		return errors.New("tipo de snapshot de código no soportado")
	}
}

// ForkWorkspace materializes a separate checkout/copy for /fork.
func (s *Store) ForkWorkspace(point *Point, destination string) (ForkResult, error) {
	if point == nil || !point.Meta.HasCode {
		return ForkResult{}, errors.New("el fork requiere un snapshot de código")
	}
	if strings.TrimSpace(destination) == "" {
		return ForkResult{}, errors.New("ruta de fork vacía")
	}
	destination, existed, err := prepareForkDestination(point.Workspace.Root, destination)
	if err != nil {
		return ForkResult{}, err
	}
	switch point.Workspace.Kind {
	case workspaceGit:
		return forkGitWorkspace(point.Workspace, destination)
	case workspaceFiles:
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return ForkResult{}, err
		}
		if err := restoreFileWorkspace(point.Workspace, s.blobDir(point.Meta.ProjectPath), destination); err != nil {
			if existed {
				_ = clearDirectory(destination)
			} else {
				_ = os.RemoveAll(destination)
			}
			return ForkResult{}, err
		}
		projectPath := destination
		if rel := filepath.Clean(point.Workspace.WorkingRel); rel != "." && rel != "" {
			projectPath = filepath.Join(destination, rel)
		}
		return ForkResult{Root: destination, ProjectPath: projectPath, Kind: workspaceFiles}, nil
	default:
		return ForkResult{}, errors.New("tipo de snapshot de código no soportado")
	}
}

func prepareForkDestination(workspaceRoot, destination string) (string, bool, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", false, fmt.Errorf("resolver ruta del fork: %w", err)
	}
	abs = filepath.Clean(abs)
	if forkPathContains(workspaceRoot, abs) {
		return "", false, fmt.Errorf("la ruta del fork no puede estar dentro del workspace original: %s", abs)
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return abs, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("la ruta del fork no es un directorio: %s", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", false, fmt.Errorf("comprobar carpeta de fork: %w", err)
	}
	if len(entries) > 0 {
		return "", false, fmt.Errorf("la carpeta del fork debe estar vacía: %s", abs)
	}
	return abs, true, nil
}

func forkPathContains(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rootAbs = resolveExistingPath(rootAbs)
	targetAbs = resolveExistingPath(targetAbs)
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func resolveExistingPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolved, filepath.Base(path))
	}
	return filepath.Clean(path)
}

func clearDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// CopyTo writes r atomically with private permissions. Kept here for fallback
// blob storage and tests.
func copyTo(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
