package todo

import (
	"reflect"
	"strings"
	"testing"
)

func strPtr(v string) *string       { return &v }
func statusPtr(v Status) *Status    { return &v }
func depsPtr(v ...string) *[]string { out := append([]string(nil), v...); return &out }
func intPtr(v int) *int             { return &v }

func initialPlan() SnapshotInput {
	return SnapshotInput{Tasks: []TaskPatch{
		{Key: "inspect", Subject: strPtr("Inspect existing implementation"), Status: statusPtr(InProgress)},
		{Key: "implement", Subject: strPtr("Implement todo state"), Status: statusPtr(Pending), DependsOn: depsPtr("inspect")},
		{Key: "verify", Subject: strPtr("Verify implementation"), Status: statusPtr(Pending), DependsOn: depsPtr("implement")},
	}}
}

func TestWriteSnapshotAtomicSparseHandoff(t *testing.T) {
	first, err := WriteSnapshot(EmptyState(), initialPlan())
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 {
		t.Fatalf("revision=%d", first.Revision)
	}
	if got := first.Tasks[1].DependsOn; !reflect.DeepEqual(got, []string{"inspect"}) {
		t.Fatalf("deps=%v", got)
	}

	second, err := WriteSnapshot(first.State, SnapshotInput{
		BaseRevision: intPtr(1),
		Tasks: []TaskPatch{
			{Key: "inspect", Status: statusPtr(Completed)},
			{Key: "implement", Status: statusPtr(InProgress)},
			{Key: "verify"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 {
		t.Fatalf("revision=%d", second.Revision)
	}
	want := []Status{Completed, InProgress, Pending}
	for i, status := range want {
		if second.Tasks[i].Status != status {
			t.Fatalf("task %d status=%s", i, second.Tasks[i].Status)
		}
	}
	if second.Tasks[1].Subject != "Implement todo state" {
		t.Fatal("sparse subject not inherited")
	}
}

func TestWriteSnapshotRejectsStaleCyclesAndBlockedStartWithoutMutation(t *testing.T) {
	first, err := WriteSnapshot(EmptyState(), initialPlan())
	if err != nil {
		t.Fatal(err)
	}
	before := cloneState(first.State)

	_, err = WriteSnapshot(first.State, SnapshotInput{BaseRevision: intPtr(0), Tasks: initialPlan().Tasks})
	if err == nil || !strings.Contains(err.Error(), "stale todo revision") {
		t.Fatalf("stale err=%v", err)
	}

	_, err = WriteSnapshot(first.State, SnapshotInput{Tasks: []TaskPatch{
		{Key: "a", Subject: strPtr("A"), Status: statusPtr(Pending), DependsOn: depsPtr("b")},
		{Key: "b", Subject: strPtr("B"), Status: statusPtr(Pending), DependsOn: depsPtr("a")},
	}})
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle err=%v", err)
	}

	_, err = WriteSnapshot(first.State, SnapshotInput{Tasks: []TaskPatch{
		{Key: "a", Subject: strPtr("A"), Status: statusPtr(Pending)},
		{Key: "b", Subject: strPtr("B"), Status: statusPtr(InProgress), DependsOn: depsPtr("a")},
	}})
	if err == nil || !strings.Contains(err.Error(), "dependencies are unresolved") {
		t.Fatalf("blocked err=%v", err)
	}

	if !reflect.DeepEqual(before, first.State) {
		t.Fatal("failed writes mutated input state")
	}
}

func TestWriteSnapshotOmissionDeletesAndClearsFields(t *testing.T) {
	desc := "Keep this"
	first, err := WriteSnapshot(EmptyState(), SnapshotInput{Tasks: []TaskPatch{
		{Key: "root", Subject: strPtr("Root"), Description: &desc, Status: statusPtr(Completed)},
		{Key: "child", Subject: strPtr("Child"), Status: statusPtr(Pending), DependsOn: depsPtr("root")},
	}})
	if err != nil {
		t.Fatal(err)
	}

	empty := ""
	noDeps := []string{}
	cleared, err := WriteSnapshot(first.State, SnapshotInput{BaseRevision: intPtr(1), Tasks: []TaskPatch{
		{Key: "root", Description: &empty},
		{Key: "child", DependsOn: &noDeps},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Tasks[0].Description != "" {
		t.Fatalf("description=%q", cleared.Tasks[0].Description)
	}
	if len(cleared.Tasks[1].DependsOn) != 0 {
		t.Fatalf("deps=%v", cleared.Tasks[1].DependsOn)
	}

	removed, err := WriteSnapshot(cleared.State, SnapshotInput{BaseRevision: intPtr(2), Tasks: []TaskPatch{{Key: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Tasks) != 1 || removed.Tasks[0].Key != "root" {
		t.Fatalf("tasks=%v", removed.Tasks)
	}
	if !reflect.DeepEqual(removed.Change.Removed, []string{"child"}) {
		t.Fatalf("removed=%v", removed.Change.Removed)
	}
}

func TestDecodeArgsPreservesOmissionVersusEmpty(t *testing.T) {
	input, err := DecodeArgs(map[string]any{
		"baseRevision": float64(2),
		"tasks": []any{
			map[string]any{"key": "a", "description": "", "dependsOn": []any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.BaseRevision == nil || *input.BaseRevision != 2 {
		t.Fatalf("base=%v", input.BaseRevision)
	}
	if input.Tasks[0].Subject != nil || input.Tasks[0].Status != nil {
		t.Fatal("omitted fields should stay nil")
	}
	if input.Tasks[0].Description == nil || *input.Tasks[0].Description != "" {
		t.Fatal("empty description must be explicit")
	}
	if input.Tasks[0].DependsOn == nil || len(*input.Tasks[0].DependsOn) != 0 {
		t.Fatal("empty deps must be explicit")
	}
}

func TestManagerRestoreRejectsCorruptState(t *testing.T) {
	m := NewManager(nil)
	bad := State{SchemaVersion: SchemaVersion, Revision: 1, Tasks: []Task{{Key: "A BAD KEY", Subject: "No", Status: Pending}}}
	if err := m.Restore(&bad); err == nil {
		t.Fatal("expected restore error")
	}
	if len(m.Snapshot().Tasks) != 0 {
		t.Fatal("invalid restore mutated manager")
	}
}

func TestRemoveCompletedTasksKeepsRequiredPrerequisites(t *testing.T) {
	state := State{SchemaVersion: SchemaVersion, Revision: 7, Tasks: []Task{
		{Key: "old", Subject: "Old finished work", Status: Completed},
		{Key: "required", Subject: "Required finished work", Status: Completed},
		{Key: "current", Subject: "Current work", Status: InProgress, DependsOn: []string{"required"}},
		{Key: "done-chain", Subject: "Finished chained work", Status: Completed, DependsOn: []string{"old"}},
	}}
	details, changed, err := RemoveCompletedTasks(state)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected cleanup to change the plan")
	}
	if details.Revision != 8 {
		t.Fatalf("revision=%d", details.Revision)
	}
	if got := SortedKeys(details.State); !reflect.DeepEqual(got, []string{"current", "required"}) {
		t.Fatalf("remaining keys=%v", got)
	}
	if !reflect.DeepEqual(details.Tasks[1].DependsOn, []string{"required"}) {
		t.Fatalf("current deps=%v", details.Tasks[1].DependsOn)
	}
}

func TestRemoveCompletedTasksNoopWhenEveryCompletedTaskIsRequired(t *testing.T) {
	state := State{SchemaVersion: SchemaVersion, Revision: 3, Tasks: []Task{
		{Key: "inspect", Subject: "Inspect", Status: Completed},
		{Key: "implement", Subject: "Implement", Status: Pending, DependsOn: []string{"inspect"}},
	}}
	details, changed, err := RemoveCompletedTasks(state)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("unexpected cleanup: %+v", details.Change)
	}
	if details.Revision != 3 || len(details.Tasks) != 2 {
		t.Fatalf("unexpected state: %+v", details.State)
	}
}
