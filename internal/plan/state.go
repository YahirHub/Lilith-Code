package plan

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

const SchemaVersion = 1

type Mode string

const (
	Build Mode = "build"
	Plan  Mode = "plan"
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []Option `json:"options,omitempty"`
}

// State is the persisted per-session operational agent state. Mode represents
// the agent selected for the NEXT user turn; an already-running turn snapshots
// its own mode when it starts, matching OpenCode's agent-cycle semantics.
type State struct {
	SchemaVersion int        `json:"schemaVersion"`
	Revision      int        `json:"revision"`
	Mode          Mode       `json:"mode"`
	LatestPlan    string     `json:"latestPlan,omitempty"`
	Ready         bool       `json:"ready,omitempty"`
	Questions     []Question `json:"questions,omitempty"`
	// HandoffPending is set only when the user moves from Plan to Build after a
	// completed plan. The next Build user turn consumes it exactly once.
	HandoffPending bool `json:"handoffPending,omitempty"`
}

type Manager struct {
	mu    sync.RWMutex
	state State
}

func NewManager(initial *State) *Manager {
	m := &Manager{}
	if err := m.Restore(initial); err != nil {
		m.Reset()
	}
	return m
}

func defaultState() State {
	return State{SchemaVersion: SchemaVersion, Mode: Build}
}

func normalize(in State) (State, error) {
	if in.SchemaVersion == 0 {
		in.SchemaVersion = SchemaVersion
	}
	if in.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported plan state schema: %d", in.SchemaVersion)
	}
	if in.Revision < 0 {
		return State{}, errors.New("plan revision cannot be negative")
	}
	if in.Mode == "" {
		in.Mode = Build
	}
	if in.Mode != Build && in.Mode != Plan {
		return State{}, fmt.Errorf("unsupported agent mode: %q", in.Mode)
	}
	in.LatestPlan = strings.TrimSpace(in.LatestPlan)
	if in.LatestPlan == "" {
		in.Ready = false
		in.HandoffPending = false
	}
	if in.Mode == Plan {
		// A handoff only makes sense once Build has been selected.
		in.HandoffPending = false
	}
	if len(in.Questions) > 3 {
		return State{}, errors.New("plan mode supports at most 3 pending questions")
	}
	seen := map[string]bool{}
	for i := range in.Questions {
		q := &in.Questions[i]
		q.ID = strings.TrimSpace(q.ID)
		q.Question = strings.TrimSpace(q.Question)
		if q.ID == "" || q.Question == "" {
			return State{}, errors.New("plan questions require id and question")
		}
		if seen[q.ID] {
			return State{}, fmt.Errorf("duplicate plan question id %q", q.ID)
		}
		seen[q.ID] = true
		if len(q.Options) > 6 {
			return State{}, fmt.Errorf("question %q has too many options", q.ID)
		}
		for j := range q.Options {
			q.Options[j].Label = strings.TrimSpace(q.Options[j].Label)
			q.Options[j].Description = strings.TrimSpace(q.Options[j].Description)
			if q.Options[j].Label == "" {
				return State{}, fmt.Errorf("question %q contains an empty option", q.ID)
			}
		}
	}
	return in, nil
}

func cloneState(in State) State {
	out := in
	if len(in.Questions) > 0 {
		out.Questions = make([]Question, len(in.Questions))
		for i := range in.Questions {
			out.Questions[i] = in.Questions[i]
			out.Questions[i].Options = append([]Option(nil), in.Questions[i].Options...)
		}
	}
	return out
}

func (m *Manager) Restore(initial *State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if initial == nil {
		m.state = defaultState()
		return nil
	}
	normalized, err := normalize(cloneState(*initial))
	if err != nil {
		return err
	}
	m.state = normalized
	return nil
}

func (m *Manager) Reset() {
	m.mu.Lock()
	m.state = defaultState()
	m.mu.Unlock()
}

func (m *Manager) Snapshot() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.state)
}

func (m *Manager) Mode() Mode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state.Mode == "" {
		return Build
	}
	return m.state.Mode
}

func (m *Manager) IsPlan() bool { return m.Mode() == Plan }

// SetMode changes the agent selected for the next turn. Switching from a ready
// Plan to Build schedules a one-shot handoff of the approved plan. Switching
// back to Plan keeps the previous plan as reference but marks it as revisable.
func (m *Manager) SetMode(mode Mode) (State, bool, error) {
	if mode != Build && mode != Plan {
		return State{}, false, fmt.Errorf("unsupported agent mode: %q", mode)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Mode == "" {
		m.state.Mode = Build
	}
	if m.state.Mode == mode {
		return cloneState(m.state), false, nil
	}
	previous := m.state.Mode
	m.state.Mode = mode
	m.state.Questions = nil
	if previous == Plan && mode == Build && m.state.Ready && m.state.LatestPlan != "" {
		m.state.HandoffPending = true
	}
	if mode == Plan {
		m.state.HandoffPending = false
		if m.state.LatestPlan != "" {
			m.state.Ready = false
		}
	}
	m.state.Revision++
	m.state.SchemaVersion = SchemaVersion
	return cloneState(m.state), true, nil
}

func (m *Manager) Toggle() (State, bool) {
	target := Plan
	if m.Mode() == Plan {
		target = Build
	}
	state, changed, _ := m.SetMode(target)
	return state, changed
}

// BeginUserTurn prepares mode-specific ephemeral state for a fresh user turn.
// Questions are considered answered by the user's next Plan message. A ready
// plan that remains in Plan mode becomes revisable rather than disappearing.
func (m *Manager) BeginUserTurn(mode Mode) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	if mode == Plan {
		if len(m.state.Questions) > 0 {
			m.state.Questions = nil
			changed = true
		}
		if m.state.Ready {
			m.state.Ready = false
			changed = true
		}
	}
	if changed {
		m.state.Revision++
	}
	return cloneState(m.state)
}

func (m *Manager) SetQuestions(questions []Question) (State, error) {
	return m.SetQuestionsFor(m.Mode(), questions)
}

// SetQuestionsFor records questions produced by a turn that SNAPSHOTTED Plan
// mode, even if the user has already pressed Tab and selected Build for the
// next turn while this one is still running.
func (m *Manager) SetQuestionsFor(turnMode Mode, questions []Question) (State, error) {
	if turnMode != Plan {
		return State{}, errors.New("plan questions are only available in Plan mode")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate := cloneState(m.state)
	candidate.Questions = append([]Question(nil), questions...)
	candidate.Ready = false
	candidate.HandoffPending = false
	candidate.Revision++
	normalized, err := normalize(candidate)
	if err != nil {
		return State{}, err
	}
	m.state = normalized
	return cloneState(m.state), nil
}

func (m *Manager) Complete(text string) (State, error) {
	return m.CompleteFor(m.Mode(), text)
}

// CompleteFor accepts the final plan from a turn that started in Plan mode.
// If Build was selected with Tab while that turn was still running, schedule
// the implementation handoff immediately for the next Build message.
func (m *Manager) CompleteFor(turnMode Mode, text string) (State, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return State{}, errors.New("plan cannot be empty")
	}
	if turnMode != Plan {
		return State{}, errors.New("plan_exit is only available in Plan mode")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LatestPlan = text
	m.state.Ready = true
	m.state.Questions = nil
	m.state.HandoffPending = m.state.Mode == Build
	m.state.Revision++
	m.state.SchemaVersion = SchemaVersion
	return cloneState(m.state), nil
}

// ConsumeBuildHandoff returns the approved plan once, on the first Build user
// turn after switching out of Plan mode.
func (m *Manager) ConsumeBuildHandoff() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Mode != Build || !m.state.HandoffPending || m.state.LatestPlan == "" {
		return "", false
	}
	plan := m.state.LatestPlan
	m.state.HandoffPending = false
	m.state.Revision++
	return plan, true
}

func (m *Manager) LatestPlan() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.LatestPlan
}
