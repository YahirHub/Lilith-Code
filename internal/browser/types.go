// Package browser provides a process-local, token-efficient Chrome DevTools
// Protocol runtime for Lilith. Chromium remains an external executable, so the
// Lilith binary can still be built with CGO_ENABLED=0.
package browser

import "time"

type ProfileMode string

const (
	ProfileTemporary  ProfileMode = "temporary"
	ProfilePersistent ProfileMode = "persistent"
	ProfileCustom     ProfileMode = "custom"
)

type Candidate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Executable      string `json:"executable,omitempty"`
	Version         string `json:"version,omitempty"`
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	RemoteURL       string `json:"remote_url,omitempty"`
	WebSocketURL    string `json:"websocket_url,omitempty"`
	UserDataDir     string `json:"user_data_dir,omitempty"`
	SafeToAttach    bool   `json:"safe_to_attach"`
	SupportsVisible bool   `json:"supports_visible"`
	Score           int    `json:"score"`
	Reason          string `json:"reason,omitempty"`
}

type Config struct {
	DefaultCandidateID string      `json:"default_candidate_id,omitempty"`
	Executable         string      `json:"executable,omitempty"`
	RemoteURL          string      `json:"remote_url,omitempty"`
	Headless           bool        `json:"headless"`
	ProfileMode        ProfileMode `json:"profile_mode"`
	ProfileName        string      `json:"profile_name,omitempty"`
	UserDataDir        string      `json:"user_data_dir,omitempty"`
}

type StartOptions struct {
	SessionID   string
	CandidateID string
	Executable  string
	RemoteURL   string
	Headless    bool
	ProfileMode ProfileMode
	ProfileName string
	UserDataDir string
	StartURL    string
}

type SessionInfo struct {
	ID             string      `json:"session_id"`
	Browser        string      `json:"browser"`
	Executable     string      `json:"executable,omitempty"`
	RemoteURL      string      `json:"remote_url,omitempty"`
	Headless       bool        `json:"headless"`
	ProfileMode    ProfileMode `json:"profile_mode"`
	UserDataDir    string      `json:"user_data_dir,omitempty"`
	CurrentTabID   string      `json:"current_tab_id,omitempty"`
	TabCount       int         `json:"tab_count"`
	StartedAt      time.Time   `json:"started_at"`
	LastActivity   time.Time   `json:"last_activity"`
	Attached       bool        `json:"attached"`
	RemoteAttached bool        `json:"remote_attached,omitempty"`
	TemporaryData  bool        `json:"temporary_profile"`
}

type TabInfo struct {
	ID       string `json:"tab_id"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Selected bool   `json:"selected"`
}

type ConsoleEvent struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

type NetworkEvent struct {
	At        time.Time `json:"at"`
	RequestID string    `json:"request_id"`
	Method    string    `json:"method,omitempty"`
	URL       string    `json:"url,omitempty"`
	Status    int64     `json:"status,omitempty"`
	MIMEType  string    `json:"mime_type,omitempty"`
	Failed    bool      `json:"failed,omitempty"`
	ErrorText string    `json:"error_text,omitempty"`
}

type ScriptInfo struct {
	ID     string `json:"script_id"`
	URL    string `json:"url,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Length int64  `json:"length,omitempty"`
}

type Element struct {
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	Role        string `json:"role,omitempty"`
	Type        string `json:"type,omitempty"`
	Name        string `json:"name,omitempty"`
	Text        string `json:"text,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Href        string `json:"href,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type Snapshot struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Text        string    `json:"text,omitempty"`
	Elements    []Element `json:"elements,omitempty"`
	Added       []Element `json:"added,omitempty"`
	Changed     []Element `json:"changed,omitempty"`
	Removed     []string  `json:"removed,omitempty"`
	Delta       bool      `json:"delta"`
	Truncated   bool      `json:"truncated,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}
