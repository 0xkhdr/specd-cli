package core

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/core/transaction"
	"github.com/0xkhdr/specd-cli/internal/plan"
	"github.com/0xkhdr/specd-cli/internal/reconcile"
)

// SyncOptions carries the human authorization inputs and the injected clock.
// Identity never arrives as a plain agent flag: ClaimedApprover must match the
// trusted identity resolved from git config user.email or SPECD_APPROVER.
type SyncOptions struct {
	GitEmail, EnvironmentApprover string
	ClaimedApprover, Reason       string
	Route                         ApprovalRoute
	Interactive, Confirmed        bool
	Now                           time.Time
	Hook                          persist.Hook
}

// SyncCapability is one reconciled accepted document as sync committed it.
type SyncCapability struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after"`
	Created    bool   `json:"created"`
	NoOp       bool   `json:"no_op"`
}

// SyncResult is the canonical sync outcome. It reports local accepted-truth
// facts only: no delivery, deployment, or remote state is implied.
type SyncResult struct {
	SchemaVersion  int              `json:"schema_version"`
	Change         string           `json:"change"`
	Approver       string           `json:"approver"`
	PlanHash       string           `json:"plan_hash"`
	EvidenceSet    string           `json:"evidence_set"`
	Capabilities   []SyncCapability `json:"capabilities"`
	TransactionID  string           `json:"transaction,omitempty"`
	HistoryID      string           `json:"history_id"`
	ArchiveTarget  string           `json:"archive_target"`
	RevisionBefore uint64           `json:"revision_before"`
	RevisionAfter  uint64           `json:"revision_after"`
	NoOp           bool             `json:"no_op"`
}

func syncFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}

// Sync makes reviewed proposed behavior accepted truth. Every precondition is
// rechecked under the change lock, the whole plan is built and validated in
// memory, and all outputs commit as one recoverable transaction. Nothing here
// bypasses approval, evidence, completion, or reconciliation: each stays a
// separate input that must independently hold.
func Sync(root, change string, options SyncOptions) (SyncResult, error) {
	// The human gate first: an agent-capable route never reaches the plan.
	approver, err := validateApproveIntent(ApproveIntent{
		GitEmail: options.GitEmail, EnvironmentApprover: options.EnvironmentApprover,
		ClaimedApprover: options.ClaimedApprover, Reason: options.Reason,
		Interactive: options.Interactive, Confirmed: options.Confirmed,
		Route: options.Route,
	})
	if err != nil {
		return SyncResult{}, err
	}
	if options.Now.IsZero() {
		return SyncResult{}, syncFailure("sync_clock", "", "sync requires an injected clock",
			"retry sync from a harness operation")
	}
	owner, err := corepath.New(root)
	if err != nil {
		return SyncResult{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return SyncResult{}, err
	}
	var result SyncResult
	// Root before change is the global lock order: the transaction owner needs
	// the root lock, so it is taken here rather than inverted underneath.
	err = lock.With(owner.RootLock(), func() error {
		return lock.With(lockPath, func() error {
			var syncErr error
			result, syncErr = syncLocked(owner, change, approver, options)
			return syncErr
		})
	})
	return result, err
}

func syncLocked(owner *corepath.Owner, change, approver string, options SyncOptions) (SyncResult, error) {
	statePath, err := owner.ChangeState(change)
	if err != nil {
		return SyncResult{}, err
	}
	raw, err := readCheckState(statePath)
	if err != nil {
		return SyncResult{}, err
	}
	current, err := state.Decode(raw, change)
	if err != nil {
		return SyncResult{}, err
	}
	// Re-running sync after it committed is a no-op success, and it must not
	// append a second semantic record.
	if current.Stage == string(LifecycleReconciling) {
		return priorSync(owner, change, options.Now)
	}
	if current.Stage != string(LifecycleApproved) {
		return SyncResult{}, syncFailure("sync_lifecycle", statePath,
			fmt.Sprintf("sync requires an approved change, got %q", current.Stage),
			"run specd status "+change)
	}

	authored := plan.ParseChange(owner, change)
	registry, err := gates.PlanningRegistry()
	if err != nil {
		return SyncResult{}, err
	}
	approval, err := projectApprovalStatusForPlanLocked(
		owner, change, registry.Version(), DefaultPolicyDigest(), authored,
	)
	if err != nil {
		return SyncResult{}, err
	}
	tasks, evidence, err := syncProofFacts(owner, change, current, authored)
	if err != nil {
		return SyncResult{}, err
	}
	reconciliation := reconcile.Build(owner, change)
	target, err := archiveTarget(owner, change, options.Now)
	if err != nil {
		return SyncResult{}, err
	}
	review := reconcile.ProjectReview(reconciliation, reconcile.ReviewInput{
		Change: authored, Approval: reconcile.ApprovalFact{
			Current: approval.Current, Reason: approval.Reason,
		},
		Tasks: tasks, Evidence: evidence,
		ArchiveTarget: filepath.ToSlash(target), ProjectionAvailable: projectionAvailable(),
	})
	if !review.Ready {
		blocker := review.Blockers[0]
		return SyncResult{}, syncFailure("sync_blocked", owner.Root(),
			blocker.Owner+" must resolve "+blocker.Code+": "+blocker.Reason, blocker.Next)
	}

	capabilities := make([]SyncCapability, 0, len(reconciliation.Capabilities))
	outputs := make([]transaction.Write, 0, len(reconciliation.Capabilities)+1)
	specs := make([]record.SpecHash, 0, len(reconciliation.Capabilities))
	for _, capability := range reconciliation.Capabilities {
		relative, err := managedRelative(owner, capability.AcceptedPath)
		if err != nil {
			return SyncResult{}, err
		}
		capabilities = append(capabilities, SyncCapability{
			Capability: capability.Capability, Path: relative,
			Before: capability.AcceptedHash, After: capability.OutputHash,
			Created: capability.Created, NoOp: capability.NoOp,
		})
		specs = append(specs, record.SpecHash{
			Capability: capability.Capability, Path: relative,
			Before: capability.AcceptedHash, After: capability.OutputHash,
		})
		outputs = append(outputs, transaction.Write{
			Path: relative, Before: capability.AcceptedHash, Bytes: capability.Output,
		})
	}
	slices.SortFunc(specs, func(a, b record.SpecHash) int { return cmp.Compare(a.Capability, b.Capability) })

	payload, err := record.NewSyncPayload(record.SyncPayload{
		Change: change, Approver: approver, ActorClass: "human",
		Reason: strings.TrimSpace(options.Reason), ApprovalHash: approval.Approval.AggregateHash,
		PlanHash: planHash(reconciliation), EvidenceSet: evidenceSetHash(current),
		Transaction: syncTransactionHash(change, current.Revision, specs),
		Outputs:     specs, RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
		Assurance: ApprovalAssuranceAdvisory,
	})
	if err != nil {
		return SyncResult{}, err
	}
	encoded, _ := json.Marshal(payload)
	history, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindSynced, Change: change,
		ExpectedRevision:  record.Revision(current.Revision),
		ResultingRevision: record.Revision(current.Revision + 1),
		Timestamp:         options.Now.UTC().Format(time.RFC3339Nano),
		Actor:             approver, Payload: encoded,
	})
	if err != nil {
		return SyncResult{}, err
	}
	stateBytes, err := advancedState(current, LifecycleReconciling, history.ID)
	if err != nil {
		return SyncResult{}, err
	}
	stateRelative, err := managedRelative(owner, statePath)
	if err != nil {
		return SyncResult{}, err
	}
	outputs = append(outputs, transaction.Write{
		Path: stateRelative, Before: hashBytes(raw), Bytes: stateBytes,
	})

	committed, err := transaction.CommitUnderRootLock(owner, transaction.Request{
		Operation: "sync", Change: change, Revision: current.Revision, Now: options.Now,
		Outputs: outputs, Record: &history, Hook: options.Hook,
	})
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{
		SchemaVersion: 1, Change: change, Approver: approver,
		PlanHash: payload.PlanHash, EvidenceSet: payload.EvidenceSet,
		Capabilities: capabilities, TransactionID: committed.ID, HistoryID: history.ID,
		ArchiveTarget:  filepath.ToSlash(target),
		RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
	}, nil
}

// priorSync answers a repeated sync from the committed record instead of
// planning again, so an unchanged retry is a no-op with no second record.
func priorSync(owner *corepath.Owner, change string, now time.Time) (SyncResult, error) {
	payload, historyID, err := latestSyncRecord(owner, change)
	if err != nil {
		return SyncResult{}, err
	}
	// Deltas are consumed: replanning a synced change would conflict on its
	// own ADDED identities. Idempotence is proved against the bytes the sync
	// record committed, not against a second reconciliation.
	if err := acceptedOutputsCurrent(owner, payload.Outputs); err != nil {
		return SyncResult{}, err
	}
	target, err := archiveTarget(owner, change, now)
	if err != nil {
		return SyncResult{}, err
	}
	capabilities := make([]SyncCapability, 0, len(payload.Outputs))
	for _, output := range payload.Outputs {
		capabilities = append(capabilities, SyncCapability{
			Capability: output.Capability, Path: output.Path,
			Before: output.Before, After: output.After, Created: output.Before == "",
			NoOp: true,
		})
	}
	return SyncResult{
		SchemaVersion: 1, Change: change, Approver: payload.Approver,
		PlanHash: payload.PlanHash, EvidenceSet: payload.EvidenceSet,
		Capabilities: capabilities, HistoryID: historyID,
		ArchiveTarget:  filepath.ToSlash(target),
		RevisionBefore: payload.RevisionBefore, RevisionAfter: payload.RevisionAfter,
		NoOp: true,
	}, nil
}

// RecoverTransactions resolves any interrupted sync or archive. Every mutating
// command runs it before its own work, so an interruption is detected and given
// its one deterministic action instead of waiting for the operation that caused
// it to be retried.
func RecoverTransactions(root string, now time.Time) error {
	owner, err := corepath.New(root)
	if err != nil {
		// Root resolution is the handler's refusal to make, not this one's.
		return nil
	}
	_, err = transaction.Recover(owner, now)
	return err
}

// acceptedOutputsCurrent proves every accepted document still holds exactly the
// bytes a committed sync wrote. Any drift blocks rather than being re-applied.
func acceptedOutputsCurrent(owner *corepath.Owner, outputs []record.SpecHash) error {
	for _, output := range outputs {
		target := filepath.Join(owner.Managed(), filepath.FromSlash(output.Path))
		raw, err := os.ReadFile(target)
		if err != nil {
			return syncFailure("sync_drift", target,
				"an accepted spec written by this sync is missing or unreadable",
				"restore .specd/specs from version control")
		}
		if hashBytes(raw) != output.After {
			return syncFailure("sync_drift", target,
				"an accepted spec changed after it was synced",
				"plan the remaining work as a new change")
		}
	}
	return nil
}

func latestSyncRecord(owner *corepath.Owner, change string) (record.SyncPayload, string, error) {
	records, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
	if err != nil {
		return record.SyncPayload{}, "", err
	}
	if len(diagnostics) != 0 {
		return record.SyncPayload{}, "", syncFailure("sync_history", owner.History(),
			"history has an incomplete tail", "restore history from version control")
	}
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Change != change || records[index].Kind != record.KindSynced {
			continue
		}
		payload, err := record.DecodeSyncPayload(records[index].Payload)
		if err != nil {
			return record.SyncPayload{}, "", syncFailure("sync_history", owner.History(),
				err.Error(), "restore history from version control")
		}
		return payload, records[index].ID, nil
	}
	return record.SyncPayload{}, "", syncFailure("sync_history", owner.History(),
		"the change is reconciling with no sync record",
		"restore history from version control")
}

// syncProofFacts projects completion and evidence as two separate facts.
// Completion proof is the durable completion binding written by `complete`,
// which already consumed current non-vacuous evidence at current HEAD.
//
// ponytail: this trusts the recorded binding rather than re-deriving evidence
// applicability at sync time, because HEAD legitimately moves between the last
// completion and sync. Upgrade path: when stage 8 adds evidence classes with
// their own staleness windows, re-derive applicability here per class.
func syncProofFacts(owner *corepath.Owner, change string, current state.State, authored plan.Change) (reconcile.TaskFact, reconcile.EvidenceFact, error) {
	graph, err := BuildTaskGraph(authored.Tasks)
	if err != nil {
		return reconcile.TaskFact{}, reconcile.EvidenceFact{}, syncFailure("sync_plan",
			authored.Tasks.Source.Path, "task graph is invalid",
			"repair tasks and run specd check "+change)
	}
	order := graph.AuthoredOrder()
	activities, err := ProjectTaskActivities(order, current.Tasks)
	if err != nil {
		return reconcile.TaskFact{}, reconcile.EvidenceFact{}, err
	}
	bindings, err := decodeCompletionBindings(current.Extensions[completionExtensionKey])
	if err != nil {
		return reconcile.TaskFact{}, reconcile.EvidenceFact{}, syncFailure("sync_state", "",
			"completion bindings are malformed", "run specd status "+change)
	}
	known, err := evidenceRecordIDs(owner)
	if err != nil {
		return reconcile.TaskFact{}, reconcile.EvidenceFact{}, err
	}
	tasks := reconcile.TaskFact{Total: len(activities)}
	var evidence reconcile.EvidenceFact
	for _, row := range activities {
		if row.Activity != TaskCompleted {
			tasks.Incomplete = append(tasks.Incomplete, row.ID)
			continue
		}
		tasks.Completed++
		binding, bound := bindings[row.ID]
		if !bound || !known[binding.EvidenceID] {
			evidence.Stale = append(evidence.Stale, row.ID)
			continue
		}
		evidence.Current++
	}
	return tasks, evidence, nil
}

func evidenceRecordIDs(owner *corepath.Owner) (map[string]bool, error) {
	items, diagnostics, err := record.Replay(owner.Evidence(), record.FamilyEvidence)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) != 0 {
		return nil, syncFailure("sync_evidence", owner.Evidence(),
			"evidence has an incomplete tail", "restore evidence from version control")
	}
	known := make(map[string]bool, len(items))
	for _, item := range items {
		known[item.ID] = true
	}
	return known, nil
}

// evidenceSetHash identifies the exact evidence consumed by every completed
// task, so history can prove which proof authorized this accepted truth.
func evidenceSetHash(current state.State) string {
	bindings, _ := decodeCompletionBindings(current.Extensions[completionExtensionKey])
	ids := make([]string, 0, len(bindings))
	for task, binding := range bindings {
		ids = append(ids, task+"\x00"+binding.EvidenceID)
	}
	slices.Sort(ids)
	return hashBytes([]byte(strings.Join(ids, "\n")))
}

// planHash identifies the exact reconciliation inputs and outputs this sync
// was authorized against. Any later byte change to a delta or accepted spec
// produces a different hash, so a stale authorization cannot be reused.
func planHash(reconciliation reconcile.Plan) string {
	rows := make([]string, 0, len(reconciliation.Capabilities))
	for _, capability := range reconciliation.Capabilities {
		rows = append(rows, strings.Join([]string{
			capability.Capability, capability.DeltaHash,
			capability.AcceptedHash, capability.OutputHash,
		}, "\x00"))
	}
	slices.Sort(rows)
	return hashBytes([]byte(strings.Join(rows, "\n")))
}

func syncTransactionHash(change string, revision uint64, specs []record.SpecHash) string {
	rows := []string{change, fmt.Sprint(revision)}
	for _, spec := range specs {
		rows = append(rows, spec.Path+"\x00"+spec.Before+"\x00"+spec.After)
	}
	return hashBytes([]byte(strings.Join(rows, "\n")))
}

// advancedState returns the encoded next state for one lifecycle transition.
func advancedState(current state.State, to Lifecycle, historyID string) ([]byte, error) {
	current.Stage = string(to)
	current.Revision++
	current.LastTransition = historyID
	return state.Encode(current)
}

func archiveTarget(owner *corepath.Owner, change string, now time.Time) (string, error) {
	target := filepath.Join(owner.Archive(), now.Format("2006-01-02")+"-"+change)
	if err := owner.CheckWriteTarget(target); err != nil {
		return "", err
	}
	return managedRelative(owner, target)
}

func managedRelative(owner *corepath.Owner, target string) (string, error) {
	relative, err := filepath.Rel(owner.Managed(), target)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", syncFailure("sync_path", target, "managed path escapes .specd",
			"restore .specd from version control")
	}
	return filepath.ToSlash(relative), nil
}

func projectionAvailable() bool {
	operation, found := OperationByID("sync")
	return found && operation.Executable
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
