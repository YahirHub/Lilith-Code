// Package goal implements a durable objective for long-running autonomous
// work. One goal belongs to one chat session/thread.
package goal

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	Active      Status = "active"
	Paused      Status = "paused"
	Blocked     Status = "blocked"
	Interrupted Status = "interrupted"
	Complete    Status = "complete"
)

const (
	legacyUsageLimited  Status = "usage_limited"
	legacyBudgetLimited Status = "budget_limited"
)

type State struct {
	Objective       string `json:"objective"`
	Status          Status `json:"status"`
	Summary         string `json:"summary,omitempty"`
	TokensUsed      int64  `json:"tokensUsed"`
	TimeUsedSeconds int64  `json:"timeUsedSeconds"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type Manager struct {
	mu          sync.RWMutex
	state       *State
	activeSince time.Time
}

func NewManager(s *State) *Manager {
	m := &Manager{}
	if s != nil {
		c := *s
		c.Status = normalizeLoadedStatus(c.Status)
		m.state = &c
		if c.Status == Active {
			m.activeSince = time.Now()
		}
	}
	return m
}

func (m *Manager) Snapshot() *State {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state == nil {
		return nil
	}
	c := *m.state
	return &c
}

func (m *Manager) Set(objective string) (*State, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return nil, errors.New("goal objective is empty")
	}
	now := time.Now()
	m.mu.Lock()
	if m.state != nil && m.state.Status == Active && m.state.Objective == objective {
		// A provider may retry a tool call after a transport hiccup. Treat the
		// exact same active goal as idempotent so usage/timing are not reset and a
		// duplicate create_goal cannot manufacture apparent progress.
		c := *m.state
		m.mu.Unlock()
		return &c, nil
	}
	s := &State{Objective: objective, Status: Active, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	m.state = s
	m.activeSince = now
	m.mu.Unlock()
	return m.Snapshot(), nil
}

func (m *Manager) Clear() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	had := m.state != nil
	m.state = nil
	m.activeSince = time.Time{}
	return had
}

func (m *Manager) UpdateStatus(status Status) error {
	if !validStatus(status) {
		return fmt.Errorf("invalid goal status %q", status)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return errors.New("no active goal")
	}
	m.accountTimeLocked()
	m.state.Status = status
	if status != Complete {
		m.state.Summary = ""
	}
	m.state.UpdatedAt = time.Now().Unix()
	if status == Active {
		m.activeSince = time.Now()
	} else {
		m.activeSince = time.Time{}
	}
	return nil
}

// Complete marks the active objective satisfied and stores the concise final
// summary that the TUI can present without parsing model prose.
func (m *Manager) Complete(summary string) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("goal completion summary is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return errors.New("no active goal")
	}
	if m.state.Status != Active {
		return fmt.Errorf("cannot complete goal with status %q", m.state.Status)
	}
	m.accountTimeLocked()
	m.state.Status = Complete
	m.state.Summary = summary
	m.state.UpdatedAt = time.Now().Unix()
	m.activeSince = time.Time{}
	return nil
}

// Resume reopens an existing paused, blocked, interrupted or completed goal
// without replacing its objective or resetting usage/creation metadata.
func (m *Manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return errors.New("no goal to resume")
	}
	if m.state.Status == Active {
		return nil
	}
	m.accountTimeLocked()
	m.state.Status = Active
	m.state.Summary = ""
	m.state.UpdatedAt = time.Now().Unix()
	m.activeSince = time.Now()
	return nil
}

// AddUsage records approximate provider usage for diagnostics only. It never
// changes the goal status and therefore cannot stop autonomous execution.
func (m *Manager) AddUsage(tokens int64) {
	if tokens <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil || m.state.Status != Active {
		return
	}
	m.accountTimeLocked()
	m.state.TokensUsed += tokens
	m.state.UpdatedAt = time.Now().Unix()
	m.activeSince = time.Now()
}

func (m *Manager) accountTimeLocked() {
	if m.state == nil || m.state.Status != Active || m.activeSince.IsZero() {
		return
	}
	d := time.Since(m.activeSince)
	if d > 0 {
		m.state.TimeUsedSeconds += int64(d / time.Second)
	}
}

func (m *Manager) Active() bool {
	s := m.Snapshot()
	return s != nil && s.Status == Active
}

func (m *Manager) PromptBlock() string {
	s := m.Snapshot()
	if s == nil {
		return ""
	}
	block := fmt.Sprintf("<durable_goal status=%q tokens_used=%q time_used_seconds=%q>\n%s\n</durable_goal>", s.Status, fmt.Sprint(s.TokensUsed), fmt.Sprint(s.TimeUsedSeconds), s.Objective)
	if strings.TrimSpace(s.Summary) != "" {
		block += "\n<goal_summary>\n" + s.Summary + "\n</goal_summary>"
	}
	if s.Status != Active {
		return block + "\nThis durable goal is not active. Do not continue it autonomously unless the user resumes it."
	}
	return block + "\nContinue working autonomously toward this durable objective without an artificial token, step, turn, or time budget. Use get_goal to inspect it and goal_complete with a concise final summary only when the objective is actually satisfied. If blocked by a material user decision, use update_goal with status=blocked and explain exactly what is needed."
}

func validStatus(s Status) bool {
	switch s {
	case Active, Paused, Blocked, Interrupted, Complete:
		return true
	}
	return false
}

func normalizeLoadedStatus(status Status) Status {
	switch status {
	case legacyUsageLimited, legacyBudgetLimited:
		// Older releases persisted artificial stop states. They are migrated back
		// to active so an existing goal is not permanently stranded after upgrade.
		return Active
	case Active, Paused, Blocked, Interrupted, Complete:
		return status
	default:
		return Paused
	}
}
