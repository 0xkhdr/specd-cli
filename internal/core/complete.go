package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const completionExtensionKey = "completions"

type CompleteRequest struct {
	TaskID           string
	ExpectedRevision uint64
	Authority        CompletionAuthority
	// Profile is empty for the default loop. Production adds blockers on top of
	// every core guard below; it never replaces or relaxes one.
	Profile          string
	PolicyTransition *PolicyTransition
	BeforeHistory    func() error
	AfterHistory     func() error
}

// CompletionAuthority is sealed to harness-owned completion operations.
type CompletionAuthority struct{ actor string }

func trustedCompletionAuthority(actor string) CompletionAuthority {
	return CompletionAuthority{actor: strings.TrimSpace(actor)}
}

// AuthorizeCompletion is the narrow harness operation boundary used by the
// command adapter. Request payloads cannot populate CompletionAuthority.
func AuthorizeCompletion(actor string) CompletionAuthority {
	return trustedCompletionAuthority(actor)
}

type Completion struct {
	SchemaVersion  int    `json:"schema_version"`
	Change         string `json:"change"`
	TaskID         string `json:"task"`
	AttemptID      string `json:"attempt"`
	EvidenceID     string `json:"evidence"`
	RevisionBefore uint64 `json:"revision_before"`
	RevisionAfter  uint64 `json:"revision_after"`
	HistoryID      string `json:"history_id"`
}

type completionBinding struct {
	AttemptID  string `json:"attempt"`
	EvidenceID string `json:"evidence"`
	HistoryID  string `json:"history"`
}

func CompleteTask(root, change string, request CompleteRequest) (Completion, error) {
	actor := strings.TrimSpace(request.Authority.actor)
	if actor == "" {
		return Completion{}, completionFailure(
			"complete_unauthorized", "", "completion actor is required",
			"retry through an authorized complete operation",
		)
	}
	policy, err := ResolveProfile(request.Profile)
	if err != nil {
		return Completion{}, err
	}
	if err := policy.Validate(); err != nil {
		return Completion{}, err
	}
	owner, err := corepath.New(root)
	if err != nil {
		return Completion{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return Completion{}, err
	}
	var result Completion
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
			return completionFailure("complete_state", statePath, err.Error(), "run status and repair state")
		}
		if current.Revision != request.ExpectedRevision {
			return completionFailure(
				"stale_revision", statePath,
				fmt.Sprintf("expected revision %d, current %d", request.ExpectedRevision, current.Revision),
				fmt.Sprintf("run status and retry from revision %d", current.Revision),
			)
		}
		return record.WithLedgerLock(owner.History(), func() error {
			history, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
			if err != nil {
				return err
			}
			if len(diagnostics) != 0 {
				return completionFailure("complete_history", owner.History(),
					"history has an incomplete tail", "repair history and run status")
			}
			pending, pendingID, found, err := recoverableCompletion(history, change, current.Revision)
			if err != nil {
				return err
			}
			if found {
				if pending.TaskID != request.TaskID {
					return completionFailure("complete_recovery", owner.History(),
						"pending completion differs from the requested task",
						"run status to recover the pending completion")
				}
				if err := validatePendingCompletionEvidence(owner.Evidence(), pending); err != nil {
					return err
				}
				result = completionResult(pending, pendingID)
				return persistCompletionState(statePath, current, pending, pendingID)
			}

			authored := plan.ParseChange(owner, change)
			graph, err := BuildTaskGraph(authored.Tasks)
			if err != nil {
				return err
			}
			task, exists := canonicalTask(authored.Tasks.Tasks, request.TaskID)
			if !exists {
				return completionFailure("complete_task", authored.Tasks.Source.Path,
					"completion task is not canonical", "run status and choose the active task")
			}
			activities, err := ProjectTaskActivities(graph.AuthoredOrder(), current.Tasks)
			if err != nil {
				return err
			}
			activityByID := make(map[string]TaskActivity, len(activities))
			for _, row := range activities {
				activityByID[row.ID] = row.Activity
			}
			if activityByID[request.TaskID] != TaskInProgress {
				return completionFailure("complete_activity", statePath,
					"task is not in progress", "run status and choose the active task")
			}
			for _, dependency := range graph.Dependencies(request.TaskID) {
				if activityByID[dependency] != TaskCompleted {
					return completionFailure("dependency_incomplete", statePath,
						fmt.Sprintf("dependency %q is incomplete", dependency),
						"complete the dependency first")
				}
			}
			attempt, exists, err := CurrentAttempt(current, request.TaskID)
			if err != nil || !exists {
				return completionFailure("complete_attempt", statePath,
					"current task attempt is missing or malformed", "regenerate task context")
			}
			if attempt.Change != change || attempt.TaskID != request.TaskID ||
				attempt.RevisionAfter != current.Revision ||
				attemptStateGuardHash(current, request.TaskID) != attempt.StateGuardHash ||
				taskContractHash(task) != attempt.ContractHash {
				return completionFailure("complete_attempt", statePath,
					"task attempt no longer matches canonical state", "regenerate task context")
			}
			if err := validateAttemptHistory(owner, current, attempt); err != nil {
				return err
			}
			registry, err := gates.PlanningRegistry()
			if err != nil {
				return err
			}
			approval, err := projectApprovalStatusForPlanLocked(
				owner, change, registry.Version(), DefaultPolicyDigest(), authored,
			)
			if err != nil {
				return err
			}
			if !approval.Current || approval.Approval == nil ||
				approval.Approval.AggregateHash != attempt.ApprovalHash {
				return completionFailure("complete_approval", statePath,
					"human approval is stale", "rerun check and obtain fresh human approval")
			}
			head, err := gitOutput(owner.Root(), "rev-parse", "--verify", "HEAD^{commit}")
			if err != nil || head != attempt.BaselineHEAD {
				return completionFailure("complete_head", owner.Root(),
					"Git HEAD no longer matches the attempt", "regenerate task context at current HEAD")
			}
			if err := validateCompletionScope(owner, change, attempt); err != nil {
				return err
			}
			command, err := verifyexec.ParseCommand(task.Verify)
			if err != nil {
				return completionFailure("complete_command", authored.Tasks.Source.Path,
					err.Error(), "repair the plan and obtain fresh human approval")
			}
			commandHash, err := verifyexec.CommandDigest(command)
			if err != nil {
				return completionFailure("complete_command", authored.Tasks.Source.Path,
					err.Error(), "repair the plan and obtain fresh human approval")
			}

			item, observation, err := latestCompletionEvidence(
				owner.Evidence(), evidence.Subject{
					Change: change, TaskID: request.TaskID, AttemptID: attempt.ID,
					HEAD: attempt.BaselineHEAD, TaskHash: taskContractHash(task),
					CommandHash: commandHash, ApprovalHash: attempt.ApprovalHash,
					StateRevision: current.Revision,
				},
			)
			if err != nil {
				return err
			}
			if item == nil || observation == nil {
				return completionFailure("complete_evidence", owner.Evidence(),
					"no current passing test-run evidence exists", "run verify")
			}
			if evidenceAlreadyConsumed(history, item.ID, pendingID) {
				return completionFailure("complete_replay", owner.History(),
					"evidence was already consumed", "run verify")
			}
			// Production runs after every core guard above, adds only blockers,
			// and never touches a task already completed under another policy.
			if policy.Production() {
				if err := validateProductionCompletion(owner, policy, request, gates.Snapshot{
					Plan: authored, Lifecycle: current.Stage, StateRevision: current.Revision,
					Transition: gates.PlanningToApproved, RegistryVersion: registry.Version(),
					PolicyDigest: policy.Digest(),
				}, evidence.Subject{
					Change: change, TaskID: request.TaskID, AttemptID: attempt.ID,
					HEAD: attempt.BaselineHEAD, TaskHash: taskContractHash(task),
					CommandHash: commandHash, ApprovalHash: attempt.ApprovalHash,
					StateRevision: current.Revision,
				}); err != nil {
					return err
				}
			}
			payload, err := record.NewCompletionPayload(record.CompletionPayload{
				Change: change, TaskID: request.TaskID, AttemptID: attempt.ID,
				EvidenceID: item.ID, Actor: actor,
				ActivityFrom: string(TaskInProgress), ActivityTo: string(TaskCompleted),
				RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
			})
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(payload)
			historyItem, err := record.New(record.Record{
				Family: record.FamilyHistory, Kind: record.KindCompletion, Change: change,
				ExpectedRevision:  record.Revision(current.Revision),
				ResultingRevision: record.Revision(current.Revision + 1),
				Timestamp:         time.Now().UTC().Format(time.RFC3339Nano),
				Actor:             actor, Payload: encoded,
			})
			if err != nil {
				return err
			}
			if request.BeforeHistory != nil {
				if err := request.BeforeHistory(); err != nil {
					return completionFailure("complete_interrupted", owner.History(), err.Error(),
						"retry complete at the same revision")
				}
			}
			// External Git/worktree writers do not honor the change lock. Check
			// both guards again at the transaction's commit boundary.
			head, err = gitOutput(owner.Root(), "rev-parse", "--verify", "HEAD^{commit}")
			if err != nil || head != attempt.BaselineHEAD {
				return completionFailure("complete_head", owner.Root(),
					"Git HEAD changed before completion commit",
					"regenerate task context at current HEAD")
			}
			if err := validateCompletionScope(owner, change, attempt); err != nil {
				return err
			}
			if err := record.AppendUnderLock(owner.History(), record.FamilyHistory, historyItem); err != nil {
				return err
			}
			if request.AfterHistory != nil {
				if err := request.AfterHistory(); err != nil {
					return completionFailure("complete_interrupted", owner.History(), err.Error(),
						"retry complete at the same revision")
				}
			}
			result = completionResult(payload, historyItem.ID)
			return persistCompletionState(statePath, current, payload, historyItem.ID)
		})
	})
	return result, err
}

// validateProductionCompletion re-evaluates the current production policy and
// its applicable proof inside the guarded completion transaction. Every refusal
// names one stable code, the failing rule or check, the subject, the observed
// state, one remediation, and the current policy digest.
func validateProductionCompletion(owner *corepath.Owner, policy Policy, request CompleteRequest, snapshot gates.Snapshot, subject evidence.Subject) error {
	digest := policy.Digest()
	if findings := gates.Production(snapshot, policy.RequiredChecks); len(findings) != 0 {
		finding := findings[0]
		return completionFailure("production_plan", finding.Location.Path,
			fmt.Sprintf("%s: %s (task %s, policy digest %s)",
				finding.Gate, finding.Problem, request.TaskID, digest),
			finding.Repair)
	}
	items, diagnostics, err := record.Replay(owner.Evidence(), record.FamilyEvidence)
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return completionFailure("production_evidence", owner.Evidence(),
			"evidence has an incomplete tail", "repair evidence and run verify")
	}
	missing, priorDigest, err := evidence.MissingProduction(
		items, policy.RequiredChecks, subject, digest,
	)
	if err != nil {
		return completionFailure("production_evidence", owner.Evidence(),
			fmt.Sprintf("%v (task %s, policy digest %s)", err, request.TaskID, digest),
			"run verify for the required production check")
	}
	if len(missing) == 0 {
		return validateReviewEvidenceSet(owner, policy, request, subject, items, digest)
	}
	if priorDigest != "" {
		if err := CheckPolicyTransition(priorDigest, policy, request.PolicyTransition); err != nil {
			return err
		}
	}
	check := missing[0]
	return completionFailure("production_evidence", owner.Evidence(),
		fmt.Sprintf("required %s proof for task %s is missing at policy digest %s",
			check.String(), request.TaskID, digest),
		"run verify for check "+check.String()+" on this task")
}

// validateReviewEvidenceSet binds every required review verdict to the exact
// evidence set it was taken against, over the records completion already
// replayed. Subject applicability cannot see this drift on its own: a verdict
// recorded before later evidence still matches code, artifact, and policy
// identity, yet an evidence-set change must stale prior review.
// ponytail: only the evidence-set binding is checked here. The packet hash is
// owned by internal/core/report, which imports this package, so completion
// cannot compute it without an import cycle; bind it too if the packet
// projection ever moves below core.
func validateReviewEvidenceSet(owner *corepath.Owner, policy Policy, request CompleteRequest, subject evidence.Subject, items []record.Record, digest string) error {
	set, err := evidence.EvidenceSetHash(items, subject.Change)
	if err != nil {
		return completionFailure("production_evidence", owner.Evidence(),
			fmt.Sprintf("%v (task %s, policy digest %s)", err, request.TaskID, digest),
			"repair evidence and run verify")
	}
	for _, check := range policy.RequiredChecks {
		if check.Class != evidence.ClassReview {
			continue
		}
		want := evidence.ProductionSubject{
			Subject: subject, Check: check, PolicyDigest: digest, EvidenceSet: set,
		}
		current := false
		for _, item := range items {
			class, classErr := evidence.ClassOf(item.Payload)
			if classErr != nil || class != evidence.ClassReview {
				continue
			}
			value, decodeErr := evidence.DecodeProduction(item.Payload)
			if decodeErr == nil && value.EvidenceSet == set && value.Applicable(want) {
				current = true
				break
			}
		}
		if !current {
			return completionFailure("production_evidence", owner.Evidence(),
				fmt.Sprintf("review proof %s for task %s is not bound to current evidence set %s at policy digest %s",
					check.String(), request.TaskID, set, digest),
				"record a fresh review for check "+check.String()+" on this task")
		}
	}
	return nil
}

func validatePendingCompletionEvidence(path string, pending record.CompletionPayload) error {
	items, diagnostics, err := record.Replay(path, record.FamilyEvidence)
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return completionFailure("complete_recovery", path,
			"pending completion evidence has an incomplete tail",
			"repair evidence and run status")
	}
	for _, item := range items {
		if item.ID != pending.EvidenceID {
			continue
		}
		// A pending completion is always bound to test-run proof; a production
		// class here is not the record it claims and fails closed below.
		if class, classErr := evidence.ClassOf(item.Payload); classErr != nil ||
			class != evidence.ClassTestRun {
			break
		}
		value, err := evidence.Decode(item.Payload)
		subject := evidence.Subject{
			Change: value.Change, TaskID: value.TaskID, AttemptID: value.AttemptID,
			HEAD: value.HEAD, TaskHash: value.TaskHash, CommandHash: value.CommandHash,
			ApprovalHash: value.ApprovalHash, StateRevision: value.StateRevision,
		}
		if err != nil || item.Change != pending.Change || value.Change != pending.Change ||
			value.TaskID != pending.TaskID || value.AttemptID != pending.AttemptID ||
			item.Timestamp != value.EndedAt || !value.Applicable(subject) {
			break
		}
		return nil
	}
	return completionFailure("complete_recovery", path,
		"pending completion is not bound to exact passing evidence",
		"repair evidence and run status")
}

func latestCompletionEvidence(path string, subject evidence.Subject) (*record.Record, *evidence.TestRun, error) {
	items, diagnostics, err := record.Replay(path, record.FamilyEvidence)
	if err != nil {
		return nil, nil, err
	}
	if len(diagnostics) != 0 {
		return nil, nil, completionFailure("complete_evidence", path,
			"evidence has an incomplete tail", "repair evidence and run verify")
	}
	var latest *record.Record
	var value *evidence.TestRun
	for index := range items {
		// Production classes are separate records with their own applicability;
		// an unknown or malformed class still fails closed here.
		class, classErr := evidence.ClassOf(items[index].Payload)
		if classErr != nil {
			return nil, nil, completionFailure("complete_evidence", path,
				"evidence contains a malformed or unknown record", "repair evidence and run verify")
		}
		if class != evidence.ClassTestRun {
			continue
		}
		decoded, err := evidence.Decode(items[index].Payload)
		if err != nil || decoded.Change != items[index].Change ||
			decoded.EndedAt != items[index].Timestamp {
			return nil, nil, completionFailure("complete_evidence", path,
				"evidence contains a malformed or unknown record", "repair evidence and run verify")
		}
		if !decoded.Matches(subject) {
			continue
		}
		latest, value = &items[index], &decoded
	}
	if value == nil || !value.Applicable(subject) {
		return nil, nil, nil
	}
	return latest, value, nil
}

func validateCompletionScope(owner *corepath.Owner, change string, attempt Attempt) error {
	changes, err := deriveScopeChanges(owner.Root(), attempt.BaselineHEAD)
	if err != nil {
		return err
	}
	declared := make(map[string]bool, len(attempt.DeclaredFiles))
	for _, file := range attempt.DeclaredFiles {
		declared[file] = true
	}
	allowed, err := validatedAttemptManagedPaths(owner, change)
	if err != nil {
		return err
	}
	var offending []string
	for _, item := range changes {
		if item.Kind != "A" && item.Kind != "M" {
			offending = append(offending, itemPaths(item)...)
			continue
		}
		for _, changedPath := range itemPaths(item) {
			if managedLockPath(changedPath) {
				continue
			}
			if managedScopePath(changedPath) {
				if !allowed[changedPath] {
					offending = append(offending, changedPath)
				}
				continue
			}
			if !declared[changedPath] {
				offending = append(offending, changedPath)
				continue
			}
			if err := rejectScopeSymlink(owner.Root(), changedPath); err != nil {
				return err
			}
			if err := rejectManagedAlias(owner.Root(), changedPath); err != nil {
				return err
			}
		}
	}
	if offending = sortedUnique(offending); len(offending) != 0 {
		return completionFailure("scope_outside", strings.Join(offending, ", "),
			"changed paths exceed exact attempt authority: "+strings.Join(offending, ", "),
			"revise the plan and obtain fresh human approval")
	}
	return nil
}

func recoverableCompletion(records []record.Record, change string, revision uint64) (record.CompletionPayload, string, bool, error) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindCompletion ||
			item.ExpectedRevision == nil || item.ResultingRevision == nil ||
			*item.ExpectedRevision != revision || *item.ResultingRevision != revision+1 {
			continue
		}
		value, err := record.DecodeCompletionPayload(item.Payload)
		if err != nil {
			return record.CompletionPayload{}, "", false, completionFailure(
				"complete_history", "", err.Error(), "repair history and run status")
		}
		return value, item.ID, true, nil
	}
	return record.CompletionPayload{}, "", false, nil
}

func evidenceAlreadyConsumed(records []record.Record, evidenceID, pendingID string) bool {
	for _, item := range records {
		if item.Kind != record.KindCompletion || item.ID == pendingID {
			continue
		}
		value, err := record.DecodeCompletionPayload(item.Payload)
		if err == nil && value.EvidenceID == evidenceID {
			return true
		}
	}
	return false
}

func persistCompletionState(path string, current state.State, value record.CompletionPayload, historyID string) error {
	bindings, err := decodeCompletionBindings(current.Extensions[completionExtensionKey])
	if err != nil {
		return err
	}
	if _, exists := bindings[value.TaskID]; exists {
		return completionFailure("complete_replay", path, "task already has completion proof", "run status")
	}
	bindings[value.TaskID] = completionBinding{
		AttemptID: value.AttemptID, EvidenceID: value.EvidenceID, HistoryID: historyID,
	}
	raw, _ := json.Marshal(bindings)
	attempts, err := decodeAttempts(current.Extensions[attemptExtensionKey])
	if err != nil {
		return err
	}
	delete(attempts, value.TaskID)
	current.Extensions[completionExtensionKey] = raw
	if len(attempts) == 0 {
		delete(current.Extensions, attemptExtensionKey)
	} else {
		raw, _ = json.Marshal(attempts)
		current.Extensions[attemptExtensionKey] = raw
	}
	current.Tasks[value.TaskID] = json.RawMessage(`"completed"`)
	current.Revision++
	current.LastTransition = historyID
	encoded, err := state.Encode(current)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(path, encoded, nil)
}

func decodeCompletionBindings(raw json.RawMessage) (map[string]completionBinding, error) {
	if len(raw) == 0 {
		return map[string]completionBinding{}, nil
	}
	var values map[string]completionBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("completion bindings have trailing data")
	}
	for key, value := range values {
		if key == "" || !completionDigest(value.AttemptID) ||
			!completionDigest(value.EvidenceID) || !completionDigest(value.HistoryID) {
			return nil, errors.New("completion binding is malformed")
		}
	}
	return values, nil
}

func completionDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func completionResult(value record.CompletionPayload, historyID string) Completion {
	return Completion{
		SchemaVersion: 1, Change: value.Change, TaskID: value.TaskID,
		AttemptID: value.AttemptID, EvidenceID: value.EvidenceID,
		RevisionBefore: value.RevisionBefore, RevisionAfter: value.RevisionAfter,
		HistoryID: historyID,
	}
}

func completionFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}
