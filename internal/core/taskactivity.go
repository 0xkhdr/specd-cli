package core

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// TaskActivity is persisted harness-owned task state. It is deliberately
// separate from derived readiness and authored Markdown.
type TaskActivity string

const (
	TaskPending    TaskActivity = "pending"
	TaskInProgress TaskActivity = "in_progress"
	TaskCompleted  TaskActivity = "completed"
	TaskFailed     TaskActivity = "failed"
	TaskBlocked    TaskActivity = "blocked"
)

var taskActivities = []TaskActivity{
	TaskPending,
	TaskInProgress,
	TaskCompleted,
	TaskFailed,
	TaskBlocked,
}

var taskActivityTransitions = map[TaskActivity][]TaskActivity{
	TaskPending:    {TaskInProgress, TaskBlocked},
	TaskInProgress: {TaskCompleted, TaskFailed},
	TaskFailed:     {TaskInProgress, TaskBlocked},
	TaskBlocked:    {TaskPending},
}

type TaskActivityProjection struct {
	ID       string       `json:"id"`
	Activity TaskActivity `json:"activity"`
}

type TaskActivityRefusal struct {
	*failure.Refusal
	Owner string
}

func (r *TaskActivityRefusal) Unwrap() error { return r.Refusal }

// TaskActivities returns the complete, stable activity vocabulary.
func TaskActivities() []TaskActivity {
	return slices.Clone(taskActivities)
}

func (activity TaskActivity) Valid() bool {
	return slices.Contains(taskActivities, activity)
}

// decodeTaskActivity decodes one persisted state.tasks value. Missing entries
// are projected by ProjectTaskActivity and are not valid encoded values.
func decodeTaskActivity(raw json.RawMessage) (TaskActivity, error) {
	var activity TaskActivity
	if err := json.Unmarshal(raw, &activity); err != nil {
		return "", fmt.Errorf("decode task activity: %w", err)
	}
	if !activity.Valid() {
		return "", fmt.Errorf("unknown task activity %q", activity)
	}
	return activity, nil
}

// ProjectTaskActivity reads only harness-owned state. Markdown markers are not
// an input and therefore cannot grant or mutate activity.
func ProjectTaskActivity(persisted map[string]json.RawMessage, taskID string) (TaskActivity, error) {
	raw, present := persisted[taskID]
	if !present {
		return TaskPending, nil
	}
	return decodeTaskActivity(raw)
}

// ProjectTaskActivities validates the complete persisted activity object
// against canonical task ids and returns rows in canonical authored order.
func ProjectTaskActivities(canonicalIDs []string, persisted map[string]json.RawMessage) ([]TaskActivityProjection, error) {
	known := make(map[string]bool, len(canonicalIDs))
	for position, id := range canonicalIDs {
		if id == "" {
			return nil, invalidTaskActivity("", "canonical task at authored position %d has empty id", position+1)
		}
		if known[id] {
			return nil, invalidTaskActivity(id, "canonical task id %q is duplicated", id)
		}
		known[id] = true
	}
	persistedIDs := make([]string, 0, len(persisted))
	for id := range persisted {
		persistedIDs = append(persistedIDs, id)
	}
	slices.Sort(persistedIDs)
	for _, id := range persistedIDs {
		if !known[id] {
			return nil, invalidTaskActivity(id, "persisted activity names unknown task %q", id)
		}
		if _, err := decodeTaskActivity(persisted[id]); err != nil {
			return nil, invalidTaskActivity(id, "task %q: %v", id, err)
		}
	}
	rows := make([]TaskActivityProjection, 0, len(canonicalIDs))
	for _, id := range canonicalIDs {
		activity, err := ProjectTaskActivity(persisted, id)
		if err != nil {
			return nil, invalidTaskActivity(id, "task %q: %v", id, err)
		}
		rows = append(rows, TaskActivityProjection{ID: id, Activity: activity})
	}
	return rows, nil
}

// CanTransitionTaskActivity is the single exhaustive owner of legal edges.
func CanTransitionTaskActivity(from, to TaskActivity) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	return slices.Contains(taskActivityTransitions[from], to)
}

// NextTaskActivities returns the legal successors in stable contract order.
func NextTaskActivities(from TaskActivity) []TaskActivity {
	if !from.Valid() {
		return nil
	}
	return slices.Clone(taskActivityTransitions[from])
}

func invalidTaskActivity(path, format string, args ...any) error {
	return &TaskActivityRefusal{
		Refusal: failure.New("plan_invalid", "", path, fmt.Sprintf(format, args...),
			"run status and repair invalid task activity"),
		Owner: "author",
	}
}
