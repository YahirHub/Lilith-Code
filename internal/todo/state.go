// Package todo implements Lilith's model-owned task plan.
//
// A todo update is an authoritative snapshot: every key included remains in
// the plan and an existing key omitted from the next snapshot is deleted.
// Existing keys may omit unchanged fields, which keeps tool calls compact.
package todo

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	SchemaVersion = 1
	MaxTasks      = 50
	MaxDeps       = 20
)

type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
)

var taskKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// Task is one resolved task in the authoritative plan.
type Task struct {
	Key         string   `json:"key"`
	Subject     string   `json:"subject"`
	Description string   `json:"description,omitempty"`
	Status      Status   `json:"status"`
	DependsOn   []string `json:"dependsOn,omitempty"`
}

// State is persisted with the chat session and injected into the system prompt
// while non-empty so a resumed conversation never relies on stale tool output.
type State struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      int    `json:"revision"`
	Tasks         []Task `json:"tasks"`
}

type Change struct {
	Added   []string `json:"added,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

type Details struct {
	State
	Change Change `json:"change"`
}

// TaskPatch intentionally uses pointers so omission differs from an explicit
// empty description/dependency list.
type TaskPatch struct {
	Key         string
	Subject     *string
	Description *string
	Status      *Status
	DependsOn   *[]string
}

type SnapshotInput struct {
	Tasks        []TaskPatch
	BaseRevision *int
}

// Manager is safe to read from the Bubble Tea renderer while a tool executes
// in a background command goroutine.
type Manager struct {
	mu    sync.RWMutex
	state State
}

func NewManager(initial *State) *Manager {
	m := &Manager{state: EmptyState()}
	if initial != nil {
		if normalized, err := NormalizeState(*initial); err == nil {
			m.state = normalized
		}
	}
	return m
}

func EmptyState() State {
	return State{SchemaVersion: SchemaVersion, Tasks: []Task{}}
}

func (m *Manager) Snapshot() State {
	if m == nil {
		return EmptyState()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.state)
}

func (m *Manager) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.state = EmptyState()
	m.mu.Unlock()
}

func (m *Manager) Restore(state *State) error {
	if m == nil {
		return nil
	}
	if state == nil {
		m.Reset()
		return nil
	}
	normalized, err := NormalizeState(*state)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.state = normalized
	m.mu.Unlock()
	return nil
}

func (m *Manager) Write(input SnapshotInput) (Details, error) {
	if m == nil {
		return Details{}, fmt.Errorf("todo state is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	details, err := WriteSnapshot(m.state, input)
	if err != nil {
		return Details{}, err
	}
	m.state = cloneState(details.State)
	return details, nil
}

func cloneTask(task Task) Task {
	out := task
	if task.DependsOn != nil {
		out.DependsOn = append([]string(nil), task.DependsOn...)
	}
	return out
}

func cloneState(state State) State {
	out := State{SchemaVersion: SchemaVersion, Revision: state.Revision, Tasks: make([]Task, len(state.Tasks))}
	for i, task := range state.Tasks {
		out.Tasks[i] = cloneTask(task)
	}
	return out
}

func normalizeKey(value, location string) (string, error) {
	key := strings.TrimSpace(value)
	if !taskKeyPattern.MatchString(key) {
		return "", fmt.Errorf("%s must be 1-40 lowercase ASCII letters, numbers, dots, underscores, or hyphens", location)
	}
	return key, nil
}

func validStatus(status Status) bool {
	return status == Pending || status == InProgress || status == Completed
}

func normalizeTask(task Task, index int) (Task, error) {
	key, err := normalizeKey(task.Key, fmt.Sprintf("tasks[%d].key", index))
	if err != nil {
		return Task{}, err
	}
	subject := strings.TrimSpace(task.Subject)
	if subject == "" {
		return Task{}, fmt.Errorf("tasks[%d].subject is required", index)
	}
	if len([]rune(subject)) > 160 {
		return Task{}, fmt.Errorf("tasks[%d].subject must be at most 160 characters", index)
	}
	description := strings.TrimSpace(task.Description)
	if len([]rune(description)) > 2000 {
		return Task{}, fmt.Errorf("tasks[%d].description must be at most 2000 characters", index)
	}
	if !validStatus(task.Status) {
		return Task{}, fmt.Errorf("tasks[%d].status is invalid: %s", index, task.Status)
	}
	if len(task.DependsOn) > MaxDeps {
		return Task{}, fmt.Errorf("tasks[%d].dependsOn supports at most %d keys", index, MaxDeps)
	}
	seen := map[string]bool{}
	deps := make([]string, 0, len(task.DependsOn))
	for depIndex, dep := range task.DependsOn {
		keyDep, err := normalizeKey(dep, fmt.Sprintf("tasks[%d].dependsOn[%d]", index, depIndex))
		if err != nil {
			return Task{}, err
		}
		if keyDep == key {
			return Task{}, fmt.Errorf("tasks[%d] cannot depend on itself (%s)", index, key)
		}
		if !seen[keyDep] {
			seen[keyDep] = true
			deps = append(deps, keyDep)
		}
	}
	return Task{Key: key, Subject: subject, Description: description, Status: task.Status, DependsOn: deps}, nil
}

func assertDependencies(tasks []Task) error {
	byKey := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byKey[task.Key] = task
	}
	for i, task := range tasks {
		for _, dep := range task.DependsOn {
			if _, ok := byKey[dep]; !ok {
				return fmt.Errorf("tasks[%d].dependsOn references missing task %s", i, dep)
			}
		}
		if task.Status != InProgress && task.Status != Completed {
			continue
		}
		var unresolved []string
		for _, dep := range task.DependsOn {
			if byKey[dep].Status != Completed {
				unresolved = append(unresolved, dep)
			}
		}
		if len(unresolved) > 0 {
			return fmt.Errorf("tasks[%d] cannot be %s while dependencies are unresolved: %s", i, task.Status, strings.Join(unresolved, ", "))
		}
	}
	return nil
}

func assertNoCycles(tasks []Task) error {
	deps := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		deps[task.Key] = task.DependsOn
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("dependency cycle detected at %s", key)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		for _, dep := range deps[key] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		delete(visiting, key)
		visited[key] = true
		return nil
	}
	for key := range deps {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func NormalizeState(state State) (State, error) {
	if state.SchemaVersion != 0 && state.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported todo schema version %d", state.SchemaVersion)
	}
	if state.Revision < 0 {
		return State{}, fmt.Errorf("todo revision cannot be negative")
	}
	if len(state.Tasks) > MaxTasks {
		return State{}, fmt.Errorf("todo supports at most %d tasks", MaxTasks)
	}
	out := State{SchemaVersion: SchemaVersion, Revision: state.Revision, Tasks: make([]Task, 0, len(state.Tasks))}
	seen := map[string]bool{}
	for i, task := range state.Tasks {
		normalized, err := normalizeTask(task, i)
		if err != nil {
			return State{}, err
		}
		if seen[normalized.Key] {
			return State{}, fmt.Errorf("tasks[%d].key is duplicated: %s", i, normalized.Key)
		}
		seen[normalized.Key] = true
		out.Tasks = append(out.Tasks, normalized)
	}
	if err := assertDependencies(out.Tasks); err != nil {
		return State{}, err
	}
	if err := assertNoCycles(out.Tasks); err != nil {
		return State{}, err
	}
	return out, nil
}

func equalTask(a, b Task) bool {
	if a.Key != b.Key || a.Subject != b.Subject || a.Description != b.Description || a.Status != b.Status || len(a.DependsOn) != len(b.DependsOn) {
		return false
	}
	for i := range a.DependsOn {
		if a.DependsOn[i] != b.DependsOn[i] {
			return false
		}
	}
	return true
}

// WriteSnapshot applies an all-or-nothing authoritative plan update.
func WriteSnapshot(state State, input SnapshotInput) (Details, error) {
	current, err := NormalizeState(state)
	if err != nil {
		return Details{}, err
	}
	if input.BaseRevision != nil && *input.BaseRevision != current.Revision {
		return Details{}, fmt.Errorf("stale todo revision: expected %d, current revision is %d", *input.BaseRevision, current.Revision)
	}
	if len(input.Tasks) > MaxTasks {
		return Details{}, fmt.Errorf("tasks supports at most %d items", MaxTasks)
	}

	existing := make(map[string]Task, len(current.Tasks))
	for _, task := range current.Tasks {
		existing[task.Key] = task
	}
	seen := map[string]bool{}
	next := make([]Task, 0, len(input.Tasks))
	for i, patch := range input.Tasks {
		key, err := normalizeKey(patch.Key, fmt.Sprintf("tasks[%d].key", i))
		if err != nil {
			return Details{}, err
		}
		if seen[key] {
			return Details{}, fmt.Errorf("tasks[%d].key is duplicated: %s", i, key)
		}
		seen[key] = true
		prev, exists := existing[key]

		subject := ""
		if patch.Subject != nil {
			subject = *patch.Subject
		} else if exists {
			subject = prev.Subject
		} else {
			return Details{}, fmt.Errorf("tasks[%d].subject is required for new task %s", i, key)
		}

		var status Status
		if patch.Status != nil {
			status = *patch.Status
		} else if exists {
			status = prev.Status
		} else {
			return Details{}, fmt.Errorf("tasks[%d].status is required for new task %s", i, key)
		}

		description := ""
		if patch.Description != nil {
			description = *patch.Description
		} else if exists {
			description = prev.Description
		}

		var deps []string
		if patch.DependsOn != nil {
			deps = append([]string(nil), (*patch.DependsOn)...)
		} else if exists {
			deps = append([]string(nil), prev.DependsOn...)
		}

		normalized, err := normalizeTask(Task{Key: key, Subject: subject, Description: description, Status: status, DependsOn: deps}, i)
		if err != nil {
			return Details{}, err
		}
		next = append(next, normalized)
	}

	// A completed task may retain a completed prerequisite purely as history.
	// If that prerequisite is omitted, prune the soft reference automatically.
	removedByKey := map[string]Task{}
	for _, old := range current.Tasks {
		if !seen[old.Key] {
			removedByKey[old.Key] = old
		}
	}
	for i := range next {
		if next[i].Status != Completed || len(next[i].DependsOn) == 0 {
			continue
		}
		filtered := next[i].DependsOn[:0]
		for _, dep := range next[i].DependsOn {
			if removed, ok := removedByKey[dep]; ok && removed.Status == Completed {
				continue
			}
			filtered = append(filtered, dep)
		}
		next[i].DependsOn = append([]string(nil), filtered...)
	}

	if err := assertDependencies(next); err != nil {
		return Details{}, err
	}
	if err := assertNoCycles(next); err != nil {
		return Details{}, err
	}

	change := Change{}
	for _, task := range next {
		old, ok := existing[task.Key]
		if !ok {
			change.Added = append(change.Added, task.Key)
		} else if !equalTask(old, task) {
			change.Updated = append(change.Updated, task.Key)
		}
	}
	for _, task := range current.Tasks {
		if !seen[task.Key] {
			change.Removed = append(change.Removed, task.Key)
		}
	}
	orderChanged := len(current.Tasks) != len(next)
	if !orderChanged {
		for i := range next {
			if current.Tasks[i].Key != next[i].Key {
				orderChanged = true
				break
			}
		}
	}
	changed := len(change.Added) > 0 || len(change.Updated) > 0 || len(change.Removed) > 0 || orderChanged
	revision := current.Revision
	if changed {
		revision++
	}
	return Details{State: State{SchemaVersion: SchemaVersion, Revision: revision, Tasks: cloneState(State{Tasks: next}).Tasks}, Change: change}, nil
}

// RemoveCompletedTasks removes completed work that is no longer a direct
// prerequisite of pending/in-progress work. It mirrors Pi's next-turn cleanup:
// finished prerequisites still needed by unfinished tasks stay in the plan,
// while historical dependency references to removed completed tasks are pruned.
func RemoveCompletedTasks(state State) (Details, bool, error) {
	current, err := NormalizeState(state)
	if err != nil {
		return Details{}, false, err
	}
	required := map[string]bool{}
	for _, task := range current.Tasks {
		if task.Status != Pending && task.Status != InProgress {
			continue
		}
		for _, dep := range task.DependsOn {
			required[dep] = true
		}
	}
	removed := map[string]bool{}
	for _, task := range current.Tasks {
		if task.Status == Completed && !required[task.Key] {
			removed[task.Key] = true
		}
	}
	if len(removed) == 0 {
		return Details{State: current}, false, nil
	}

	patches := make([]TaskPatch, 0, len(current.Tasks)-len(removed))
	for _, task := range current.Tasks {
		if removed[task.Key] {
			continue
		}
		subject := task.Subject
		description := task.Description
		status := task.Status
		deps := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if !removed[dep] {
				deps = append(deps, dep)
			}
		}
		patches = append(patches, TaskPatch{
			Key: task.Key, Subject: &subject, Description: &description,
			Status: &status, DependsOn: &deps,
		})
	}
	revision := current.Revision
	details, err := WriteSnapshot(current, SnapshotInput{Tasks: patches, BaseRevision: &revision})
	if err != nil {
		return Details{}, false, err
	}
	return details, true, nil
}

// CleanupCompleted applies next-turn cleanup atomically to the manager.
func (m *Manager) CleanupCompleted() (Details, bool, error) {
	if m == nil {
		return Details{}, false, fmt.Errorf("todo state is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	details, changed, err := RemoveCompletedTasks(m.state)
	if err != nil {
		return Details{}, false, err
	}
	if changed {
		m.state = cloneState(details.State)
	}
	return details, changed, nil
}

// DecodeArgs converts the generic JSON map used by the tool registry while
// preserving omitted fields versus explicit empty values.
func DecodeArgs(args map[string]any) (SnapshotInput, error) {
	rawTasks, ok := args["tasks"]
	if !ok {
		return SnapshotInput{}, fmt.Errorf("tasks is required")
	}
	items, ok := rawTasks.([]any)
	if !ok {
		return SnapshotInput{}, fmt.Errorf("tasks must be an array")
	}
	input := SnapshotInput{Tasks: make([]TaskPatch, 0, len(items))}
	if rawRev, exists := args["baseRevision"]; exists && rawRev != nil {
		rev, err := integer(rawRev)
		if err != nil || rev < 0 {
			return SnapshotInput{}, fmt.Errorf("baseRevision must be a non-negative integer")
		}
		input.BaseRevision = &rev
	}
	for i, raw := range items {
		obj, ok := raw.(map[string]any)
		if !ok {
			return SnapshotInput{}, fmt.Errorf("tasks[%d] must be an object", i)
		}
		key, ok := obj["key"].(string)
		if !ok {
			return SnapshotInput{}, fmt.Errorf("tasks[%d].key is required", i)
		}
		patch := TaskPatch{Key: key}
		if value, exists := obj["subject"]; exists {
			s, ok := value.(string)
			if !ok {
				return SnapshotInput{}, fmt.Errorf("tasks[%d].subject must be a string", i)
			}
			patch.Subject = &s
		}
		if value, exists := obj["description"]; exists {
			s, ok := value.(string)
			if !ok {
				return SnapshotInput{}, fmt.Errorf("tasks[%d].description must be a string", i)
			}
			patch.Description = &s
		}
		if value, exists := obj["status"]; exists {
			s, ok := value.(string)
			if !ok {
				return SnapshotInput{}, fmt.Errorf("tasks[%d].status must be a string", i)
			}
			status := Status(s)
			patch.Status = &status
		}
		if value, exists := obj["dependsOn"]; exists {
			rawDeps, ok := value.([]any)
			if !ok {
				return SnapshotInput{}, fmt.Errorf("tasks[%d].dependsOn must be an array", i)
			}
			deps := make([]string, 0, len(rawDeps))
			for j, rawDep := range rawDeps {
				dep, ok := rawDep.(string)
				if !ok {
					return SnapshotInput{}, fmt.Errorf("tasks[%d].dependsOn[%d] must be a string", i, j)
				}
				deps = append(deps, dep)
			}
			patch.DependsOn = &deps
		}
		input.Tasks = append(input.Tasks, patch)
	}
	return input, nil
}

func integer(value any) (int, error) {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	fromInt64 := func(n int64) (int, error) {
		if n < minInt || n > maxInt {
			return 0, fmt.Errorf("integer out of range")
		}
		return int(n), nil
	}
	switch n := value.(type) {
	case int:
		return n, nil
	case int64:
		return fromInt64(n)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return 0, fmt.Errorf("not integer")
		}
		// float64 cannot represent MaxInt64 exactly. Use the first value beyond
		// the signed range as the exclusive bound on 64-bit builds.
		if ^uint(0)>>63 == 1 {
			if n < -9223372036854775808.0 || n >= 9223372036854775808.0 {
				return 0, fmt.Errorf("integer out of range")
			}
		} else if n < -2147483648.0 || n > 2147483647.0 {
			return 0, fmt.Errorf("integer out of range")
		}
		return int(n), nil
	case json.Number:
		v, err := n.Int64()
		if err != nil {
			return 0, err
		}
		return fromInt64(v)
	default:
		return 0, fmt.Errorf("not integer")
	}
}

// FormatForModel returns the exact authoritative state after a write.
func FormatForModel(state State) string {
	if len(state.Tasks) == 0 {
		return fmt.Sprintf("Todo plan cleared (revision %d).", state.Revision)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Todo plan revision %d:\n", state.Revision)
	for _, task := range state.Tasks {
		fmt.Fprintf(&b, "[%s] %s: %s", task.Status, task.Key, task.Subject)
		if task.Description != "" {
			fmt.Fprintf(&b, " — %s", task.Description)
		}
		if len(task.DependsOn) > 0 {
			fmt.Fprintf(&b, " <- %s", strings.Join(task.DependsOn, ","))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// PromptBlock is deliberately compact but exact. It makes persisted todo state
// branch/session-safe without requiring Pi's extension-specific hidden messages.
func PromptBlock(state State) string {
	if len(state.Tasks) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n<todo_state revision=\"%d\">\n", state.Revision)
	for _, task := range state.Tasks {
		fmt.Fprintf(&b, "[%s] %s: %s", task.Status, task.Key, task.Subject)
		if task.Description != "" {
			fmt.Fprintf(&b, " — %s", task.Description)
		}
		if len(task.DependsOn) > 0 {
			fmt.Fprintf(&b, " <- %s", strings.Join(task.DependsOn, ","))
		}
		b.WriteByte('\n')
	}
	b.WriteString("</todo_state>")
	return b.String()
}

// SortedKeys is useful in tests/debugging without exposing internal maps.
func SortedKeys(state State) []string {
	keys := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		keys = append(keys, task.Key)
	}
	sort.Strings(keys)
	return keys
}
