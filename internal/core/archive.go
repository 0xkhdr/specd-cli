package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/core/transaction"
)

// ArchiveOptions carries the acting identity and the injected local clock. The
// clock is injected because the archive prefix is a local calendar date, which
// the harness must not read from the machine's UTC offset by accident.
type ArchiveOptions struct {
	Actor string
	Now   time.Time
	Hook  persist.Hook
}

// ArchiveResult reports one completed local archive. It names no external
// system: archive never deploys, commits, pushes, or opens anything.
type ArchiveResult struct {
	SchemaVersion  int      `json:"schema_version"`
	Change         string   `json:"change"`
	Source         string   `json:"source"`
	Target         string   `json:"target"`
	ChangeHash     string   `json:"change_hash"`
	Accepted       []string `json:"accepted"`
	EvidenceSet    string   `json:"evidence_set"`
	Approver       string   `json:"approver"`
	SyncRecord     string   `json:"sync_record"`
	TransactionID  string   `json:"transaction"`
	HistoryID      string   `json:"history_id"`
	RevisionBefore uint64   `json:"revision_before"`
	RevisionAfter  uint64   `json:"revision_after"`
}

func archiveFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}

// Archive validates the whole change before moving a single byte, then moves
// the complete change folder as one recoverable transaction.
func Archive(root, change string, options ArchiveOptions) (ArchiveResult, error) {
	if strings.TrimSpace(options.Actor) == "" {
		return ArchiveResult{}, archiveFailure("archive_unauthorized", "",
			"archive actor is required", "retry through an authorized harness operation")
	}
	if options.Now.IsZero() {
		return ArchiveResult{}, archiveFailure("archive_clock", "",
			"archive requires an injected clock", "retry archive from a harness operation")
	}
	owner, err := corepath.New(root)
	if err != nil {
		return ArchiveResult{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ArchiveResult{}, err
	}
	var result ArchiveResult
	err = lock.With(owner.RootLock(), func() error {
		return lock.With(lockPath, func() error {
			var archiveErr error
			result, archiveErr = archiveLocked(owner, change, options)
			return archiveErr
		})
	})
	return result, err
}

func archiveLocked(owner *corepath.Owner, change string, options ArchiveOptions) (ArchiveResult, error) {
	statePath, err := owner.ChangeState(change)
	if err != nil {
		return ArchiveResult{}, err
	}
	raw, err := readCheckState(statePath)
	if err != nil {
		return ArchiveResult{}, err
	}
	current, err := state.Decode(raw, change)
	if err != nil {
		return ArchiveResult{}, err
	}
	if current.Stage != string(LifecycleReconciling) {
		return ArchiveResult{}, archiveFailure("archive_lifecycle", statePath,
			fmt.Sprintf("archive requires a reconciled change, got %q", current.Stage),
			"run specd sync "+change+" before archiving")
	}

	// The sync record is the durable authorization this archive inherits. It
	// names the human approver, the artifact hashes that approval covered, and
	// the evidence set that proved the work.
	synced, syncRecordID, err := latestSyncRecord(owner, change)
	if err != nil {
		return ArchiveResult{}, err
	}
	changeRoot, err := owner.Change(change)
	if err != nil {
		return ArchiveResult{}, err
	}
	if err := requireCurrentApprovalHash(changeRoot, synced.ApprovalHash); err != nil {
		return ArchiveResult{}, err
	}
	if evidenceSetHash(current) != synced.EvidenceSet {
		return ArchiveResult{}, archiveFailure("archive_evidence", owner.Evidence(),
			"the completed evidence set changed after sync",
			"plan the remaining work as a new change")
	}
	if err := requireBoundEvidencePresent(owner, current); err != nil {
		return ArchiveResult{}, err
	}
	if err := requireEveryTaskCompleted(current); err != nil {
		return ArchiveResult{}, err
	}

	// Accepted truth must still be exactly what the authorized sync wrote.
	// The deltas that produced it are already consumed, so replanning them
	// would conflict with their own applied identities; the committed output
	// hashes are the honest proof that the deltas are synchronized.
	if err := acceptedOutputsCurrent(owner, synced.Outputs); err != nil {
		return ArchiveResult{}, archiveFailure("archive_unsynced", owner.Specs(),
			err.Error(), "plan the remaining work as a new change")
	}
	accepted := make([]record.SpecHash, 0, len(synced.Outputs))
	names := make([]string, 0, len(synced.Outputs))
	for _, output := range synced.Outputs {
		accepted = append(accepted, record.SpecHash{
			Capability: output.Capability, Path: output.Path, After: output.After,
		})
		names = append(names, output.Capability)
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].Capability < accepted[j].Capability })
	sort.Strings(names)

	source, err := managedRelative(owner, changeRoot)
	if err != nil {
		return ArchiveResult{}, err
	}
	target, err := archiveTarget(owner, change, options.Now)
	if err != nil {
		return ArchiveResult{}, err
	}
	absoluteTarget := filepath.Join(owner.Managed(), filepath.FromSlash(target))
	if _, err := os.Lstat(absoluteTarget); err == nil {
		return ArchiveResult{}, archiveFailure("archive_target_exists", absoluteTarget,
			"the archive target already exists",
			"inspect the existing archived change")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ArchiveResult{}, archiveFailure("archive_target_unreadable", absoluteTarget,
			err.Error(), "repair the archive path from version control")
	}
	changeHash, err := hashChangeFolder(changeRoot)
	if err != nil {
		return ArchiveResult{}, err
	}

	payload, err := record.NewArchivePayload(record.ArchivePayload{
		Change: change, Actor: strings.TrimSpace(options.Actor), Approver: synced.Approver,
		Source: source, Target: target, ChangeHash: changeHash, Accepted: accepted,
		EvidenceSet: synced.EvidenceSet, SyncRecord: syncRecordID,
		Transaction:    hashBytes([]byte(source + "\n" + target + "\n" + changeHash)),
		RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
	})
	if err != nil {
		return ArchiveResult{}, err
	}
	encoded, _ := json.Marshal(payload)
	history, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindArchived, Change: change,
		ExpectedRevision:  record.Revision(current.Revision),
		ResultingRevision: record.Revision(current.Revision + 1),
		Timestamp:         options.Now.UTC().Format(time.RFC3339Nano),
		Actor:             payload.Actor, Payload: encoded,
	})
	if err != nil {
		return ArchiveResult{}, err
	}
	stateBytes, err := advancedState(current, LifecycleArchived, history.ID)
	if err != nil {
		return ArchiveResult{}, err
	}
	stateRelative, err := managedRelative(owner, statePath)
	if err != nil {
		return ArchiveResult{}, err
	}
	committed, err := transaction.CommitUnderRootLock(owner, transaction.Request{
		Operation: "archive", Change: change, Revision: current.Revision, Now: options.Now,
		Outputs: []transaction.Write{{Path: stateRelative, Before: hashBytes(raw), Bytes: stateBytes}},
		Moves:   []transaction.Move{{From: source, To: target}},
		Record:  &history, Hook: options.Hook,
	})
	if err != nil {
		return ArchiveResult{}, err
	}
	return ArchiveResult{
		SchemaVersion: 1, Change: change, Source: source, Target: target,
		ChangeHash: changeHash, Accepted: names, EvidenceSet: synced.EvidenceSet,
		Approver: synced.Approver, SyncRecord: syncRecordID,
		TransactionID: committed.ID, HistoryID: history.ID,
		RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
	}, nil
}

// requireCurrentApprovalHash re-derives the artifact hashes the human approval
// covered. Any byte change to a covered artifact invalidates that approval, so
// a change edited after sync cannot be archived on the old authorization.
func requireCurrentApprovalHash(changeRoot, recorded string) error {
	covered, err := currentApprovalCoveredPaths(changeRoot)
	if err != nil {
		return archiveFailure("archive_approval", changeRoot,
			"the approved artifact set is unreadable or unsafe",
			"restore the planning artifacts from version control")
	}
	identity, err := ComputeApprovalIdentity(changeRoot, covered)
	if err != nil {
		return archiveFailure("archive_approval", changeRoot, err.Error(),
			"restore the planning artifacts from version control")
	}
	if identity.AggregateHash != recorded {
		return archiveFailure("archive_approval", changeRoot,
			"planning artifacts changed after the authorized sync",
			"plan the remaining work as a new change")
	}
	return nil
}

// requireBoundEvidencePresent proves the evidence each completion consumed is
// still in the ledger. Archive never repairs it and never deletes it: a
// missing record blocks the move.
func requireBoundEvidencePresent(owner *corepath.Owner, current state.State) error {
	bindings, err := decodeCompletionBindings(current.Extensions[completionExtensionKey])
	if err != nil {
		return archiveFailure("archive_state", "", "completion bindings are malformed",
			"restore the change state from version control")
	}
	known, err := evidenceRecordIDs(owner)
	if err != nil {
		return err
	}
	missing := make([]string, 0, len(bindings))
	for task, binding := range bindings {
		if !known[binding.EvidenceID] {
			missing = append(missing, task)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return archiveFailure("archive_evidence", owner.Evidence(),
		"evidence is missing for "+strings.Join(missing, ", "),
		"restore evidence from version control")
}

func requireEveryTaskCompleted(current state.State) error {
	incomplete := make([]string, 0, len(current.Tasks))
	for id, activity := range current.Tasks {
		if string(activity) != `"completed"` {
			incomplete = append(incomplete, id)
		}
	}
	if len(incomplete) == 0 {
		return nil
	}
	sort.Strings(incomplete)
	return archiveFailure("archive_incomplete", "",
		"tasks are not complete: "+strings.Join(incomplete, ", "),
		"complete the remaining tasks before archiving")
}

// hashChangeFolder identifies the exact bytes being preserved. Lock files are
// excluded: they carry no state and their presence changes no decision.
func hashChangeFolder(changeRoot string) (string, error) {
	var rows []string
	err := filepath.WalkDir(changeRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(changeRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".lock" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("change folder contains " + relative + " which is not a regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rows = append(rows, relative+"\x00"+hashBytes(raw))
		return nil
	})
	if err != nil {
		return "", archiveFailure("archive_unsafe_source", changeRoot, err.Error(),
			"restore the change folder from version control")
	}
	sort.Strings(rows)
	return hashBytes([]byte(strings.Join(rows, "\n"))), nil
}
