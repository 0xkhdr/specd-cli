package core

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestTaskActivityExactVocabulary(t *testing.T) {
	want := []TaskActivity{
		TaskPending,
		TaskInProgress,
		TaskCompleted,
		TaskFailed,
		TaskBlocked,
	}
	if got := TaskActivities(); !reflect.DeepEqual(got, want) {
		t.Fatalf("activities = %q, want %q", got, want)
	}
	got := TaskActivities()
	got[0] = "mutated"
	if TaskActivities()[0] != TaskPending {
		t.Fatal("caller mutated activity vocabulary")
	}
	for _, activity := range want {
		if !activity.Valid() {
			t.Fatalf("contract activity %q is invalid", activity)
		}
	}
	for _, activity := range []TaskActivity{"", "running", "complete", "future"} {
		if activity.Valid() {
			t.Fatalf("unknown activity %q is valid", activity)
		}
	}
}

func TestTaskActivityGeneratedSequencesMatchIndependentTable(t *testing.T) {
	legal := map[TaskActivity][]TaskActivity{
		TaskPending:    {TaskInProgress, TaskBlocked},
		TaskInProgress: {TaskCompleted, TaskFailed},
		TaskFailed:     {TaskInProgress, TaskBlocked},
		TaskBlocked:    {TaskPending},
	}
	activities := []TaskActivity{TaskPending, TaskInProgress, TaskCompleted, TaskFailed, TaskBlocked, "future"}
	random := rand.New(rand.NewSource(20260806))
	for sequence := 0; sequence < 1000; sequence++ {
		current := activities[random.Intn(len(activities))]
		for step := 0; step < 20; step++ {
			next := activities[random.Intn(len(activities))]
			want := false
			for _, allowed := range legal[current] {
				want = want || allowed == next
			}
			if got := CanTransitionTaskActivity(current, next); got != want {
				t.Fatalf("sequence %d step %d: %q to %q = %v, want %v", sequence, step, current, next, got, want)
			}
			if want {
				current = next
			}
		}
	}
}

func TestTaskActivityTransitionMatrixIsExhaustive(t *testing.T) {
	legal := map[[2]TaskActivity]bool{
		{TaskPending, TaskInProgress}:   true,
		{TaskInProgress, TaskCompleted}: true,
		{TaskInProgress, TaskFailed}:    true,
		{TaskFailed, TaskInProgress}:    true,
		{TaskPending, TaskBlocked}:      true,
		{TaskFailed, TaskBlocked}:       true,
		{TaskBlocked, TaskPending}:      true,
	}
	activities := TaskActivities()
	accepted := 0
	for _, from := range activities {
		for _, to := range activities {
			got := CanTransitionTaskActivity(from, to)
			want := legal[[2]TaskActivity{from, to}]
			if got != want {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, want)
			}
			if got {
				accepted++
			}
		}
	}
	if accepted != 7 {
		t.Fatalf("accepted %d transitions, want 7", accepted)
	}
	for _, pair := range [][2]TaskActivity{
		{"future", TaskPending},
		{TaskPending, "future"},
		{"", ""},
	} {
		if CanTransitionTaskActivity(pair[0], pair[1]) {
			t.Fatalf("unknown transition %q -> %q accepted", pair[0], pair[1])
		}
	}
}

func TestTaskActivitySuccessorsMatchMatrix(t *testing.T) {
	want := map[TaskActivity][]TaskActivity{
		TaskPending:    {TaskInProgress, TaskBlocked},
		TaskInProgress: {TaskCompleted, TaskFailed},
		TaskCompleted:  nil,
		TaskFailed:     {TaskInProgress, TaskBlocked},
		TaskBlocked:    {TaskPending},
	}
	for activity, successors := range want {
		if got := NextTaskActivities(activity); !reflect.DeepEqual(got, successors) {
			t.Fatalf("successors(%s) = %q, want %q", activity, got, successors)
		}
	}
	if NextTaskActivities("future") != nil {
		t.Fatal("future activity exposes successors")
	}
	got := NextTaskActivities(TaskPending)
	got[0] = TaskCompleted
	if NextTaskActivities(TaskPending)[0] != TaskInProgress {
		t.Fatal("caller mutated transition table")
	}
}

func TestTaskActivityProjectionDefaultsAndRejectsUnknown(t *testing.T) {
	persisted := map[string]json.RawMessage{
		"T2": json.RawMessage(`"blocked"`),
	}
	if got, err := ProjectTaskActivity(persisted, "T1"); err != nil || got != TaskPending {
		t.Fatalf("new task = %q, %v; want pending", got, err)
	}
	if got, err := ProjectTaskActivity(persisted, "T2"); err != nil || got != TaskBlocked {
		t.Fatalf("persisted task = %q, %v; want blocked", got, err)
	}
	for name, raw := range map[string]json.RawMessage{
		"future":    json.RawMessage(`"paused"`),
		"empty":     json.RawMessage(`""`),
		"object":    json.RawMessage(`{"activity":"pending"}`),
		"malformed": json.RawMessage(`"`),
		"trailing":  json.RawMessage(`"pending" "blocked"`),
	} {
		if _, err := ProjectTaskActivity(map[string]json.RawMessage{"T1": raw}, "T1"); err == nil {
			t.Errorf("%s activity accepted", name)
		}
	}
}

func TestTaskActivityProjectsAllCanonicalRows(t *testing.T) {
	rows, err := ProjectTaskActivities(
		[]string{"Z", "A", "M"},
		map[string]json.RawMessage{"A": json.RawMessage(`"in_progress"`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []TaskActivityProjection{
		{ID: "Z", Activity: TaskPending},
		{ID: "A", Activity: TaskInProgress},
		{ID: "M", Activity: TaskPending},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
	rows[0].Activity = TaskCompleted
	again, err := ProjectTaskActivities([]string{"Z"}, nil)
	if err != nil || again[0].Activity != TaskPending {
		t.Fatalf("caller mutated projection: %#v, %v", again, err)
	}
}

func TestTaskActivityFullProjectionFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		ids       []string
		persisted map[string]json.RawMessage
	}{
		{name: "empty canonical id", ids: []string{""}},
		{name: "duplicate canonical id", ids: []string{"T1", "T1"}},
		{name: "orphaned persisted id", ids: []string{"T1"}, persisted: map[string]json.RawMessage{
			"T2": json.RawMessage(`"pending"`),
		}},
		{name: "future persisted value", ids: []string{"T1"}, persisted: map[string]json.RawMessage{
			"T1": json.RawMessage(`"paused"`),
		}},
		{name: "malformed persisted value", ids: []string{"T1"}, persisted: map[string]json.RawMessage{
			"T1": json.RawMessage(`{"activity":"pending"}`),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := ProjectTaskActivities(test.ids, test.persisted)
			if err == nil || rows != nil || !failure.IsCode(err, "plan_invalid") {
				t.Fatalf("projection = %#v, %v; want nil plan_invalid", rows, err)
			}
			refusal, ok := err.(*TaskActivityRefusal)
			if !ok || refusal.Owner != "author" ||
				refusal.Next != "run status and repair invalid task activity" {
				t.Fatalf("refusal = %#v, want stable author-owned refusal", err)
			}
		})
	}
}

func TestTaskActivityMarkdownMarkerIsNonAuthoritative(t *testing.T) {
	persisted := map[string]json.RawMessage{"T1": json.RawMessage(`"failed"`)}
	// The fixed seven-column tasks table has no checkbox/activity column.
	// Marker-like prose elsewhere in Markdown is intentionally not an input.
	first, firstErr := ProjectTaskActivity(persisted, "T1")
	second, secondErr := ProjectTaskActivity(persisted, "T1")
	if firstErr != nil || secondErr != nil || first != TaskFailed || second != first {
		t.Fatalf("marker changed activity: before=%q/%v after=%q/%v", first, firstErr, second, secondErr)
	}
}
