// Package goal implements a durable Codex-style objective for long-running
// autonomous work. One goal belongs to one chat session/thread.
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
	Active        Status = "active"
	Paused        Status = "paused"
	Blocked       Status = "blocked"
	UsageLimited  Status = "usage_limited"
	BudgetLimited Status = "budget_limited"
	Complete      Status = "complete"
)

type State struct {
	Objective       string `json:"objective"`
	Status          Status `json:"status"`
	TokenBudget     *int64 `json:"tokenBudget,omitempty"`
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
	if m.state.TokenBudget != nil {
		v := *m.state.TokenBudget
		c.TokenBudget = &v
	}
	return &c
}
func (m *Manager) Set(objective string, budget *int64) (*State, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return nil, errors.New("goal objective is empty")
	}
	if budget != nil && *budget <= 0 {
		return nil, errors.New("token budget must be positive")
	}
	now := time.Now()
	s := &State{Objective: objective, Status: Active, TokenBudget: budget, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	m.mu.Lock()
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
	m.state.UpdatedAt = time.Now().Unix()
	if status == Active {
		m.activeSince = time.Now()
	} else {
		m.activeSince = time.Time{}
	}
	return nil
}
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
	if m.state.TokenBudget != nil && m.state.TokensUsed >= *m.state.TokenBudget {
		m.state.Status = BudgetLimited
		m.activeSince = time.Time{}
	} else {
		m.activeSince = time.Now()
	}
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
func (m *Manager) Active() bool { s := m.Snapshot(); return s != nil && s.Status == Active }
func (m *Manager) PromptBlock() string {
	s := m.Snapshot()
	if s == nil {
		return ""
	}
	budget := "unbounded"
	if s.TokenBudget != nil {
		budget = fmt.Sprintf("%d", *s.TokenBudget)
	}
	block := fmt.Sprintf("<durable_goal status=%q tokens_used=%q token_budget=%q time_used_seconds=%q>\n%s\n</durable_goal>", s.Status, fmt.Sprint(s.TokensUsed), budget, fmt.Sprint(s.TimeUsedSeconds), s.Objective)
	if s.Status != Active {
		return block + "\nThis durable goal is not active. Do not continue it autonomously unless the user resumes it."
	}
	return block + "\nContinue working autonomously toward this durable objective. Use get_goal to inspect it and update_goal with status=complete only when the objective is actually satisfied. If blocked by a material user decision, set status=blocked and explain exactly what is needed."
}
func validStatus(s Status) bool {
	switch s {
	case Active, Paused, Blocked, UsageLimited, BudgetLimited, Complete:
		return true
	}
	return false
}
