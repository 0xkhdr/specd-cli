package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const taskTransitionSchemaVersion = 1

type TaskTransitionRequest struct {
	TaskID           string
	To               TaskActivity
	Authority        TaskTransitionAuthority
	ExpectedRevision uint64
	AfterHistory     func() error
}

// TaskTransitionAuthority is sealed to core-owned operation authorization.
// External request payloads cannot manufacture it from actor strings/booleans.
type TaskTransitionAuthority struct{ actor string }

func trustedTaskTransitionAuthority(actor string) TaskTransitionAuthority {
	return TaskTransitionAuthority{actor: strings.TrimSpace(actor)}
}

type TaskTransition struct {
	SchemaVersion int          `json:"schema_version"`
	TaskID        string       `json:"task"`
	From          TaskActivity `json:"from"`
	To            TaskActivity `json:"to"`
}

func TransitionTaskActivity(root, change string, request TaskTransitionRequest) (TaskTransition, error) {
	actor := strings.TrimSpace(request.Authority.actor)
	if actor == "" {
		return TaskTransition{}, taskTransitionFailure(
			"task_unauthorized", "", "task transition actor is unauthorized",
			"request the transition through an authorized harness operation",
		)
	}
	owner, err := corepath.New(root)
	if err != nil {
		return TaskTransition{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return TaskTransition{}, err
	}
	var transitioned TaskTransition
	err = lock.With(lockPath, func() error {
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		raw, err := readCheckState(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(raw, change)
		if err != nil {
			return taskTransitionFailure("plan_invalid", statePath, err.Error(), "run status and repair state")
		}
		if current.Revision != request.ExpectedRevision {
			return taskTransitionFailure(
				"stale_revision", statePath,
				fmt.Sprintf("expected revision %d, current %d", request.ExpectedRevision, current.Revision),
				fmt.Sprintf("run status and retry from revision %d", current.Revision),
			)
		}

		history, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
		if err != nil {
			return err
		}
		if len(diagnostics) != 0 {
			return taskTransitionFailure(
				"task_history", owner.History(), "history has an incomplete tail",
				"repair history and run status",
			)
		}
		recovered, recoveryID, recoveryFound, err := recoverableTaskTransition(
			history, change, current.Revision,
		)
		if err != nil {
			return err
		}

		authored := plan.ParseChange(owner, change).Tasks
		graph, err := BuildTaskGraph(authored)
		if err != nil {
			return err
		}
		if _, exists := graph.Wave(request.TaskID); !exists {
			return taskTransitionFailure(
				"task_unknown", authored.Source.Path, fmt.Sprintf("task %q is not canonical", request.TaskID),
				"choose a task reported by status",
			)
		}
		activities, err := ProjectTaskActivities(graph.AuthoredOrder(), current.Tasks)
		if err != nil {
			return err
		}
		activityByID := make(map[string]TaskActivity, len(activities))
		for _, activity := range activities {
			activityByID[activity.ID] = activity.Activity
		}
		from := activityByID[request.TaskID]
		if !CanTransitionTaskActivity(from, request.To) {
			return taskTransitionFailure(
				"task_transition", statePath,
				fmt.Sprintf("task %q cannot transition from %q to %q", request.TaskID, from, request.To),
				taskTransitionNextAction(request.TaskID, from),
			)
		}
		if request.To == TaskCompleted {
			return taskTransitionFailure(
				"task_evidence_required", statePath, "task completion requires current passing evidence",
				"run verify and complete through the stage 5 harness",
			)
		}
		registry, err := gates.PlanningRegistry()
		if err != nil {
			return err
		}
		approval, err := projectApprovalStatusLocked(owner, change, registry.Version(), DefaultPolicyDigest())
		if err != nil {
			return err
		}
		if request.To == TaskInProgress &&
			!taskCanStart(request.TaskID, from, authored, current, approval) {
			return taskTransitionFailure(
				"task_not_ready", statePath, fmt.Sprintf("task %q is not in the current frontier", request.TaskID),
				"run status and choose a ready task",
			)
		}
		if recoveryFound {
			if recovered.TaskID != request.TaskID || recovered.From != from ||
				recovered.To != request.To || historyActor(history, recoveryID) != actor {
				return taskTransitionFailure(
					"task_recovery", owner.History(), "pending task transition differs from current request or state",
					"run status to recover the pending transition",
				)
			}
			transitioned = recovered
			return persistTaskTransitionState(statePath, current, recovered, recoveryID)
		}

		transitioned = TaskTransition{
			SchemaVersion: taskTransitionSchemaVersion,
			TaskID:        request.TaskID, From: from, To: request.To,
		}
		payload, _ := json.Marshal(transitioned)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		historyRecord, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindTaskTransition, Change: change,
			ExpectedRevision:  record.Revision(current.Revision),
			ResultingRevision: record.Revision(current.Revision + 1),
			Timestamp:         now, Actor: actor, Payload: payload,
		})
		if err != nil {
			return err
		}
		if err := record.Append(owner.History(), record.FamilyHistory, historyRecord); err != nil {
			return err
		}
		if request.AfterHistory != nil {
			if err := request.AfterHistory(); err != nil {
				return taskTransitionFailure(
					"task_interrupted", owner.History(), err.Error(),
					"run status to recover the pending transition",
				)
			}
		}
		return persistTaskTransitionState(statePath, current, transitioned, historyRecord.ID)
	})
	return transitioned, err
}

// taskCanStart asks the one readiness owner rather than re-deriving lifecycle,
// approval, or dependency rules here.
func taskCanStart(
	id string,
	from TaskActivity,
	tasks plan.Tasks,
	current state.State,
	approval ApprovalStatus,
) bool {
	model := ProjectReadiness(tasks, current.Tasks, current.Stage, approval)
	switch from {
	case TaskPending:
		return model.InFrontier(id)
	case TaskFailed:
		return model.CanRetry(id)
	default:
		return false
	}
}

func recoverableTaskTransition(records []record.Record, change string, revision uint64) (TaskTransition, string, bool, error) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindTaskTransition ||
			item.ExpectedRevision == nil || item.ResultingRevision == nil ||
			*item.ExpectedRevision != revision || *item.ResultingRevision != revision+1 {
			continue
		}
		transition, err := decodeTaskTransition(item.Payload)
		if err != nil {
			return TaskTransition{}, "", false, taskTransitionFailure(
				"task_history", "", err.Error(), "repair history and run status",
			)
		}
		return transition, item.ID, true, nil
	}
	return TaskTransition{}, "", false, nil
}

func decodeTaskTransition(raw json.RawMessage) (TaskTransition, error) {
	var transition TaskTransition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transition); err != nil {
		return TaskTransition{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return TaskTransition{}, errors.New("task transition payload has trailing data")
	}
	if transition.SchemaVersion != taskTransitionSchemaVersion ||
		transition.TaskID == "" || !transition.From.Valid() ||
		!transition.To.Valid() || !CanTransitionTaskActivity(transition.From, transition.To) {
		return TaskTransition{}, errors.New("task transition payload is invalid")
	}
	return transition, nil
}

func historyActor(records []record.Record, id string) string {
	for _, item := range records {
		if item.ID == id {
			return item.Actor
		}
	}
	return ""
}

func persistTaskTransitionState(path string, current state.State, transition TaskTransition, historyID string) error {
	raw, err := json.Marshal(transition.To)
	if err != nil {
		return err
	}
	current.Tasks[transition.TaskID] = raw
	current.Revision++
	current.LastTransition = historyID
	encoded, err := state.Encode(current)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(path, encoded, nil)
}

func taskTransitionNextAction(id string, from TaskActivity) string {
	next := NextTaskActivities(from)
	if len(next) == 0 {
		return fmt.Sprintf("run status; task %q has no legal successor", id)
	}
	values := make([]string, len(next))
	for index := range next {
		values[index] = string(next[index])
	}
	return fmt.Sprintf("run status and transition task %q to one of: %s", id, strings.Join(values, ", "))
}

func taskTransitionFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}
