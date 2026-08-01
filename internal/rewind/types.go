// Package rewind persists conversation checkpoints and matching workspace
// snapshots for /rewind and /fork.
package rewind

import (
	"time"

	"github.com/lilith/li/internal/session"
)

const (
	workspaceNone  = ""
	workspaceGit   = "git"
	workspaceFiles = "files"
)

// Meta is the lightweight row rendered by the /rewind selector.
type Meta struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"sessionId"`
	ProjectPath string    `json:"projectPath"`
	Prompt      string    `json:"prompt"`
	Kind        string    `json:"kind,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	HasCode     bool      `json:"hasCode"`
	PartialCode bool      `json:"partialCode,omitempty"`
	CodeError   string    `json:"codeError,omitempty"`
	FileCount   int       `json:"fileCount,omitempty"`
	TotalBytes  int64     `json:"totalBytes,omitempty"`
}

// Point stores the exact conversation state immediately before a user action.
// The workspace field is populated lazily before the first mutating tool of
// that turn, avoiding a repository scan for read-only conversations.
type Point struct {
	Meta         Meta              `json:"meta"`
	Conversation *session.Session  `json:"conversation"`
	Workspace    WorkspaceSnapshot `json:"workspace,omitempty"`
}

// WorkspaceSnapshot identifies either an immutable Git tree/commit or a
// content-addressed fallback manifest for non-Git projects.
type WorkspaceSnapshot struct {
	Kind       string         `json:"kind,omitempty"`
	Root       string         `json:"root,omitempty"`
	WorkingRel string         `json:"workingRel,omitempty"`
	GitCommit  string         `json:"gitCommit,omitempty"`
	GitRef     string         `json:"gitRef,omitempty"`
	Files      []FileEntry    `json:"files,omitempty"`
	Skipped    []SkippedEntry `json:"skipped,omitempty"`
}

// FileEntry is one path in a fallback snapshot.
type FileEntry struct {
	Path       string `json:"path"`
	Mode       uint32 `json:"mode,omitempty"`
	Size       int64  `json:"size,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Symlink    bool   `json:"symlink,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
}

// SkippedEntry records paths that could not be included in a fallback
// snapshot. The restore remains available, but the selector warns that it is
// partial rather than promising an exact rewind.
type SkippedEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ForkResult describes the isolated workspace created from a checkpoint.
type ForkResult struct {
	Root        string
	ProjectPath string
	Kind        string
}
