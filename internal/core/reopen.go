package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

type ReopenIntent struct {
	ExpectedRevision uint64
	Actor            string
	Reason           string
	AfterHistory     func() error
}

type ReopenResult struct {
	record.ReopenedPayload
	HistoryID string `json:"history_id"`
}

// Reopen revokes execution authority without granting replacement authority.
func Reopen(root, change string, intent ReopenIntent) (ReopenResult, error) {
	intent.Actor, intent.Reason = strings.TrimSpace(intent.Actor), strings.TrimSpace(intent.Reason)
	if intent.Actor == "" || intent.Reason == "" {
		return ReopenResult{}, reopenFailure("reopen_intent", "", "actor and reason are required", "supply a non-empty reason and retry")
	}
	owner, err := corepath.New(root)
	if err != nil {
		return ReopenResult{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ReopenResult{}, err
	}
	var result ReopenResult
	err = lock.With(lockPath, func() error {
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(raw, change)
		if err != nil {
			return err
		}
		if current.Revision != intent.ExpectedRevision {
			return reopenFailure("stale_revision", statePath,
				fmt.Sprintf("expected revision %d, current %d", intent.ExpectedRevision, current.Revision),
				fmt.Sprintf("run status and retry from revision %d", current.Revision))
		}
		if current.Stage != string(LifecycleApproved) {
			switch current.Stage {
			case string(LifecyclePlanning):
				return reopenFailure("reopen_lifecycle", statePath, "change is already planning", "continue authoring and run check")
			default:
				return reopenFailure("reopen_lifecycle", statePath,
					fmt.Sprintf("cannot reopen lifecycle %q", current.Stage), "plan any further behavior as a new change")
			}
		}

		records, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
		if err != nil {
			return err
		}
		if len(diagnostics) != 0 {
			return reopenFailure("reopen_history", owner.History(), "history has an incomplete tail", "repair history and run status")
		}
		approval, approvalHistoryID, found, err := latestApproval(records, change)
		if err != nil {
			return err
		}
		if !found || !reopenApprovalMatchesState(current, approval) {
			return reopenFailure("reopen_history", owner.History(), "approved state is not bound to exact approval history", "repair history and run status")
		}
		attemptID, err := reopenAttemptID(records, current)
		if err != nil {
			return err
		}
		head, err := cleanGitBaseline(owner.Root(), nil)
		if err != nil {
			return err
		}
		payload, err := record.NewReopenedPayload(record.ReopenedPayload{
			Change: change, Actor: intent.Actor, Reason: intent.Reason,
			LifecycleFrom: string(LifecycleApproved), LifecycleTo: string(LifecyclePlanning),
			ApprovalID: approval.ID, AttemptID: attemptID, ObservedHEAD: head,
			RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
		})
		if err != nil {
			return err
		}
		if prior, id, ok, err := recoverableReopen(records, change, current.Revision); err != nil {
			return err
		} else if ok {
			if prior != payload {
				return reopenFailure("reopen_recovery", owner.History(), "pending reopen differs from current request or state", "run status to recover the pending transition")
			}
			result = ReopenResult{ReopenedPayload: prior, HistoryID: id}
			return persistReopenedState(statePath, current, id)
		}
		if !approvalActivityTailCurrent(records, change, approvalHistoryID, approval, current) {
			return reopenFailure("reopen_history", owner.History(), "approved state is not bound to exact history", "repair history and run status")
		}
		encoded, _ := json.Marshal(payload)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		history, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindReopened, Change: change,
			ExpectedRevision: record.Revision(current.Revision), ResultingRevision: record.Revision(current.Revision + 1),
			Timestamp: now, Actor: intent.Actor, Payload: encoded,
		})
		if err != nil {
			return err
		}
		if err := record.Append(owner.History(), record.FamilyHistory, history); err != nil {
			return err
		}
		if intent.AfterHistory != nil {
			if err := intent.AfterHistory(); err != nil {
				return reopenFailure("reopen_interrupted", owner.History(), err.Error(), "retry reopen to recover the pending transition")
			}
		}
		result = ReopenResult{ReopenedPayload: payload, HistoryID: history.ID}
		return persistReopenedState(statePath, current, history.ID)
	})
	return result, err
}

func reopenApprovalMatchesState(current state.State, approval ApprovalRecord) bool {
	if len(current.Approvals) == 0 {
		return false
	}
	var persisted ApprovalRecord
	decoder := json.NewDecoder(bytes.NewReader(current.Approvals[len(current.Approvals)-1]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&persisted) != nil || decoder.Decode(&struct{}{}) != io.EOF || persisted.Validate() != nil {
		return false
	}
	return persisted.ID == approval.ID
}

func reopenAttemptID(records []record.Record, current state.State) (string, error) {
	attempts, err := decodeAttempts(current.Extensions[attemptExtensionKey])
	if err != nil || len(attempts) > 1 {
		return "", reopenFailure("reopen_state", "", "active attempt state is malformed or ambiguous", "repair state and run status")
	}
	for _, attempt := range attempts {
		for _, item := range records {
			if item.ID != current.LastTransition || item.Kind != record.KindAttempt {
				continue
			}
			recorded, decodeErr := record.DecodeAttemptPayload(item.Payload)
			if decodeErr == nil && recorded.ID == attempt.ID {
				return attempt.ID, nil
			}
		}
		return "", reopenFailure("reopen_history", "", "active attempt is not bound to exact history", "repair history and run status")
	}
	return "", nil
}

func recoverableReopen(records []record.Record, change string, revision uint64) (record.ReopenedPayload, string, bool, error) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindReopened || item.ExpectedRevision == nil ||
			item.ResultingRevision == nil || *item.ExpectedRevision != revision || *item.ResultingRevision != revision+1 {
			continue
		}
		payload, err := record.DecodeReopenedPayload(item.Payload)
		if err != nil {
			return record.ReopenedPayload{}, "", false, reopenFailure("reopen_history", "", err.Error(), "repair history and run status")
		}
		return payload, item.ID, true, nil
	}
	return record.ReopenedPayload{}, "", false, nil
}

func persistReopenedState(path string, current state.State, historyID string) error {
	current.Stage = string(LifecyclePlanning)
	current.Tasks = map[string]json.RawMessage{}
	delete(current.Extensions, attemptExtensionKey)
	delete(current.Extensions, completionExtensionKey)
	current.Revision++
	current.LastTransition = historyID
	raw, err := state.Encode(current)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(path, raw, nil)
}

func reopenFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}
