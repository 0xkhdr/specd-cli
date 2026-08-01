package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type ApprovalStatus struct {
	Change           string          `json:"change"`
	Current          bool            `json:"current"`
	Approval         *ApprovalRecord `json:"approval,omitempty"`
	BlockingArtifact string          `json:"blocking_artifact,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Recovery         string          `json:"recovery,omitempty"`
}

func CurrentApprovalStatus(root, change string) (ApprovalStatus, error) {
	registry, err := gates.PlanningRegistry()
	if err != nil {
		return ApprovalStatus{}, err
	}
	return ProjectApprovalStatus(root, change, registry.Version(), DefaultPolicyDigest())
}

// ProjectApprovalStatus accepts policy identities explicitly so callers can
// project staleness when a deployed registry or policy changes.
func ProjectApprovalStatus(root, change, registryVersion, policyDigest string) (ApprovalStatus, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return ApprovalStatus{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ApprovalStatus{}, err
	}
	var status ApprovalStatus
	err = lock.With(lockPath, func() error {
		var projectErr error
		status, projectErr = projectApprovalStatusLocked(owner, change, registryVersion, policyDigest)
		return projectErr
	})
	return status, err
}

func projectApprovalStatusLocked(owner *corepath.Owner, change, registryVersion, policyDigest string) (ApprovalStatus, error) {
	authored := plan.ParseChange(owner, change)
	return projectApprovalStatusForPlanLocked(owner, change, registryVersion, policyDigest, authored)
}

func projectApprovalStatusForPlanLocked(owner *corepath.Owner, change, registryVersion, policyDigest string, authored plan.Change) (ApprovalStatus, error) {
	status := ApprovalStatus{Change: change}
	statePath, err := owner.ChangeState(change)
	if err != nil {
		return status, err
	}
	raw, err := readCheckState(statePath)
	if err != nil {
		return status, err
	}
	current, err := state.Decode(raw, change)
	if err != nil {
		return status, err
	}
	records, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
	if err != nil {
		return status, err
	}
	if len(diagnostics) != 0 {
		return status, fmt.Errorf("history has incomplete record at line %d", diagnostics[0].Line)
	}
	approval, historyID, found, err := latestApproval(records, change)
	if err != nil {
		return status, err
	}
	if !found {
		stale(&status, "approval_missing", "", "run check, then obtain fresh human approval")
		return status, nil
	}
	status.Approval = &approval
	if current.Stage != string(approval.LifecycleTo) {
		stale(&status, "approval_not_applicable", "", "rerun check, then obtain fresh human approval")
		return status, nil
	}
	if !approvalActivityTailCurrent(records, change, historyID, approval, current) {
		stale(&status, "state_revision_changed", "", "rerun check, then obtain fresh human approval")
		return status, nil
	}
	if registryVersion != approval.RegistryVersion {
		stale(&status, "registry_version_changed", "", "rerun check, then obtain fresh human approval")
		return status, nil
	}
	if policyDigest != approval.PolicyDigest {
		stale(&status, "policy_digest_changed", "", "rerun check, then obtain fresh human approval")
		return status, nil
	}
	changeRoot, err := owner.Change(change)
	if err != nil {
		return status, err
	}
	currentPaths, err := statusCoveredPaths(changeRoot, authored)
	if err != nil {
		return status, err
	}
	recordedPaths := make([]string, len(approval.Artifacts))
	for index := range approval.Artifacts {
		recordedPaths[index] = approval.Artifacts[index].Path
	}
	if path := firstPathDifference(recordedPaths, currentPaths); path != "" {
		stale(&status, "artifact_set_changed", path, "restore planning artifacts, rerun check, then obtain fresh human approval")
		return status, nil
	}
	identity, err := approvalIdentityFromPlan(changeRoot, authored)
	if err != nil {
		stale(&status, "artifact_missing_or_unsafe", firstAbsentPlanArtifact(changeRoot, authored), "restore the artifact, rerun check, then obtain fresh human approval")
		return status, nil
	}
	for index, artifact := range approval.Artifacts {
		if identity.Artifacts[index].Hash != artifact.Hash {
			stale(&status, "artifact_changed", artifact.Path, "rerun check, then obtain fresh human approval")
			return status, nil
		}
	}
	status.Current = true
	return status, nil
}

func firstAbsentPlanArtifact(changeRoot string, authored plan.Change) string {
	sources := []plan.Source{authored.Proposal.Source, authored.Design.Source, authored.Tasks.Source}
	for _, delta := range authored.Deltas {
		sources = append(sources, delta.Source)
	}
	for _, source := range sources {
		if source.Present {
			continue
		}
		relative, err := filepath.Rel(changeRoot, source.Path)
		if err == nil {
			return filepath.ToSlash(relative)
		}
	}
	return ""
}

func approvalActivityTailCurrent(records []record.Record, change, approvalHistoryID string, approval ApprovalRecord, current state.State) bool {
	index := -1
	for position := range records {
		if records[position].ID == approvalHistoryID {
			index = position
			break
		}
	}
	if index < 0 {
		return false
	}
	revision := approval.RevisionAfter
	lastTransition := approvalHistoryID
	for _, item := range records[index+1:] {
		if item.Change != change {
			continue
		}
		switch item.Kind {
		case record.KindTaskTransition:
			if _, err := decodeTaskTransition(item.Payload); err != nil {
				return false
			}
		case record.KindAttempt:
			if _, err := decodeAttempt(item.Payload); err != nil {
				return false
			}
		case record.KindCompletion:
			if _, err := record.DecodeCompletionPayload(item.Payload); err != nil {
				return false
			}
		case record.KindFriction:
			// An observation carries no transition, so it cannot participate in
			// this revision chain and must not stale the approval that
			// authorized the work it observed.
			continue
		default:
			return false
		}
		// A single append-first task record may be waiting for transaction
		// recovery. It is not yet applicable state and must not stale the
		// approval that authorized its attempt.
		if item.ExpectedRevision != nil &&
			*item.ExpectedRevision == current.Revision &&
			current.LastTransition == lastTransition {
			break
		}
		if item.ExpectedRevision == nil || item.ResultingRevision == nil ||
			*item.ExpectedRevision != revision || *item.ResultingRevision != revision+1 {
			return false
		}
		revision++
		lastTransition = item.ID
	}
	return current.Revision == revision && current.LastTransition == lastTransition
}

func latestApproval(records []record.Record, change string) (ApprovalRecord, string, bool, error) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindApproved {
			continue
		}
		var approval ApprovalRecord
		decoder := json.NewDecoder(bytes.NewReader(item.Payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&approval); err != nil {
			return ApprovalRecord{}, "", false, fmt.Errorf("decode approval history: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return ApprovalRecord{}, "", false, errors.New("decode approval history: multiple JSON values")
		}
		if err := validateApprovalEnvelope(item, approval); err != nil {
			return ApprovalRecord{}, "", false, err
		}
		return approval, item.ID, true, nil
	}
	return ApprovalRecord{}, "", false, nil
}

func validateApprovalEnvelope(item record.Record, approval ApprovalRecord) error {
	if err := approval.Validate(); err != nil {
		return fmt.Errorf("validate approval history: %w", err)
	}
	if item.Change != approval.Change ||
		item.ExpectedRevision == nil || item.ResultingRevision == nil ||
		*item.ExpectedRevision != approval.RevisionBefore ||
		*item.ResultingRevision != approval.RevisionAfter ||
		item.Actor != approval.Approver ||
		item.Timestamp != approval.Timestamp {
		return errors.New("approval history envelope does not match payload")
	}
	return nil
}

func statusCoveredPaths(changeRoot string, change plan.Change) ([]string, error) {
	sources := []string{change.Proposal.Source.Path, change.Design.Source.Path, change.Tasks.Source.Path}
	for _, delta := range change.Deltas {
		sources = append(sources, delta.Source.Path)
	}
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		relative, err := filepath.Rel(changeRoot, source)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("planning artifact escapes change root")
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	sort.Strings(paths)
	return paths, nil
}

func firstPathDifference(recorded, current []string) string {
	recorded = append([]string(nil), recorded...)
	current = append([]string(nil), current...)
	sort.Strings(recorded)
	sort.Strings(current)
	for index := 0; index < len(recorded) || index < len(current); index++ {
		if index >= len(recorded) {
			return current[index]
		}
		if index >= len(current) {
			return recorded[index]
		}
		if recorded[index] != current[index] {
			if recorded[index] < current[index] {
				return recorded[index]
			}
			return current[index]
		}
	}
	return ""
}

func stale(status *ApprovalStatus, reason, artifact, recovery string) {
	status.Current = false
	status.Reason = reason
	status.BlockingArtifact = artifact
	status.Recovery = recovery
}
