package core

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type ApproveIntent struct {
	GitEmail, EnvironmentApprover string
	ClaimedApprover, Reason       string
	Interactive, Confirmed        bool
	Route                         ApprovalRoute
	AfterHistory                  func() error
}

func ResolveApprovalIdentity(gitEmail, environment string) (string, error) {
	gitEmail, environment = strings.TrimSpace(gitEmail), strings.TrimSpace(environment)
	if gitEmail == "" && environment == "" {
		return "", failure.New("approval_identity", "", "", "trusted identity is missing", "set git user.email or SPECD_APPROVER")
	}
	if gitEmail != "" && environment != "" && gitEmail != environment {
		return "", failure.New("approval_identity", "", "", "trusted identity sources disagree", "make git user.email and SPECD_APPROVER agree")
	}
	if environment != "" {
		return environment, nil
	}
	return gitEmail, nil
}

func validateApproveIntent(intent ApproveIntent) (string, error) {
	if intent.Route != ApprovalRouteHumanTerminal {
		return "", ApprovalHumanOnlyFacts().AuthorizeAgentCapableRoute()
	}
	identity, err := ResolveApprovalIdentity(intent.GitEmail, intent.EnvironmentApprover)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(intent.Reason) == "" {
		return "", failure.New("approval_intent", "", "", "approval reason is missing", "supply a non-empty reason")
	}
	if intent.Interactive {
		if !intent.Confirmed {
			return "", failure.New("approval_intent", "", "", "approval was not confirmed", "confirm approval in the human terminal")
		}
		if intent.ClaimedApprover != "" && intent.ClaimedApprover != identity {
			return "", failure.New("approval_identity", "", "", "claimed approver differs from trusted identity", "remove or correct the approver claim")
		}
		return identity, nil
	}
	if strings.TrimSpace(intent.ClaimedApprover) == "" {
		return "", failure.New("approval_intent", "", "", "non-interactive approver is missing", "supply --approver matching trusted identity")
	}
	if intent.ClaimedApprover != identity {
		return "", failure.New("approval_identity", "", "", "claimed approver differs from trusted identity", "supply --approver matching trusted identity")
	}
	return identity, nil
}

func Approve(root, change string, intent ApproveIntent) (ApprovalRecord, error) {
	approver, err := validateApproveIntent(intent)
	if err != nil {
		return ApprovalRecord{}, err
	}
	owner, err := corepath.New(root)
	if err != nil {
		return ApprovalRecord{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ApprovalRecord{}, err
	}
	var approved ApprovalRecord
	err = lock.With(lockPath, func() error {
		snapshot, err := assembleCheckLocked(owner, change, DefaultPolicyDigest())
		if err != nil {
			return err
		}
		// Lifecycle is decided before gate findings: a change another approver
		// already advanced needs no authoring repair, so the loser of a
		// concurrent approval must be told the next legal lifecycle action
		// rather than sent to fix findings that are not defects.
		if snapshot.snapshot.Lifecycle != "planning" {
			return failure.New("lifecycle_transition", owner.Root(), "", "approval requires planning lifecycle", "continue with the next legal lifecycle action")
		}
		result := EvaluateCheck(snapshot)
		if !result.Success {
			return failure.New("approval_gates", owner.Root(), "", "planning gates failed", "repair findings and rerun check")
		}
		changeRoot, _ := owner.Change(change)
		covered, err := approvalCoveredPaths(changeRoot, snapshot)
		if err != nil {
			return err
		}
		identity, err := approvalIdentityFromPlan(changeRoot, snapshot.snapshot.Plan)
		if err != nil {
			return err
		}
		statePath, _ := owner.ChangeState(change)
		currentRaw, err := os.ReadFile(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(currentRaw, change)
		if err != nil {
			return err
		}
		if current.Revision != snapshot.snapshot.StateRevision || current.Stage != "planning" {
			return failure.New("stale_revision", owner.Root(), statePath,
				"approval snapshot no longer matches planning state",
				"rerun check, then obtain fresh human approval")
		}
		currentCovered, coverageErr := currentApprovalCoveredPaths(changeRoot)
		again, err := ComputeApprovalIdentity(changeRoot, covered)
		slices.Sort(covered)
		if coverageErr != nil || !slices.Equal(covered, currentCovered) {
			return failure.New("approval_drift", owner.Root(), "", "covered artifact set changed during approval", "rerun check, then approve fresh content")
		}
		if err != nil || again.AggregateHash != identity.AggregateHash {
			return failure.New("approval_drift", owner.Root(), "", "covered artifacts changed during approval", "rerun check, then approve fresh content")
		}
		records, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
		if err != nil {
			return err
		}
		if len(diagnostics) != 0 {
			return failure.New("approval_history", owner.Root(), owner.History(), "history has an incomplete tail", "restore history, rerun check, then approve")
		}
		if prior, historyID, ok := recoverableApproval(records, change, current.Revision); ok {
			if prior.AggregateHash != identity.AggregateHash ||
				prior.RegistryVersion != snapshot.registry.Version() ||
				prior.PolicyDigest != snapshot.snapshot.PolicyDigest {
				return failure.New("approval_recovery", owner.Root(), owner.History(), "interrupted approval no longer matches current inputs", "rerun check, then approve fresh content")
			}
			if prior.Approver != approver {
				return failure.New("approval_recovery", owner.Root(), owner.History(),
					fmt.Sprintf("interrupted approval belongs to another approver %q", prior.Approver),
					fmt.Sprintf("have %s complete the interrupted approval", prior.Approver))
			}
			approved = prior
			return persistApprovalState(statePath, current, approved, historyID)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		approved = ApprovalRecord{
			SchemaVersion: ApprovalSchemaVersion, ID: fmt.Sprintf("approval:%s:%d", identity.AggregateHash[:16], current.Revision),
			Change: change, Gate: "planning_to_approved", LifecycleFrom: LifecyclePlanning, LifecycleTo: LifecycleApproved,
			Approver: approver, ActorClass: "human", Artifacts: identity.Artifacts, AggregateHash: identity.AggregateHash,
			RegistryVersion: snapshot.registry.Version(), PolicyDigest: snapshot.snapshot.PolicyDigest,
			RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1, Timestamp: now,
			Reason: strings.TrimSpace(intent.Reason), Assurance: "advisory",
		}
		if err := approved.Validate(); err != nil {
			return err
		}
		payload, _ := json.Marshal(approved)
		history, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindApproved, Change: change,
			ExpectedRevision: record.Revision(current.Revision), ResultingRevision: record.Revision(current.Revision + 1),
			Timestamp: now, Actor: approver, Payload: payload,
		})
		if err != nil {
			return err
		}
		if err := record.Append(owner.History(), record.FamilyHistory, history); err != nil {
			return err
		}
		if intent.AfterHistory != nil {
			if err := intent.AfterHistory(); err != nil {
				return err
			}
		}
		return persistApprovalState(statePath, current, approved, history.ID)
	})
	return approved, err
}

func currentApprovalCoveredPaths(changeRoot string) ([]string, error) {
	covered := []string{"proposal.md", "design.md", "tasks.md"}
	specs := filepath.Join(changeRoot, "specs")
	entries, err := os.ReadDir(specs)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return nil, errors.New("delta artifact set is unsafe")
		}
		children, err := os.ReadDir(filepath.Join(specs, entry.Name()))
		if err != nil || len(children) != 1 || children[0].Name() != "spec.md" ||
			children[0].Type()&os.ModeSymlink != 0 || children[0].IsDir() {
			return nil, errors.New("delta artifact set is unsafe")
		}
		covered = append(covered, filepath.ToSlash(filepath.Join("specs", entry.Name(), "spec.md")))
	}
	slices.Sort(covered)
	return covered, nil
}

func approvalIdentityFromPlan(changeRoot string, authored plan.Change) (ApprovalIdentity, error) {
	sources := []plan.Source{
		authored.Proposal.Source, authored.Design.Source, authored.Tasks.Source,
	}
	for _, delta := range authored.Deltas {
		sources = append(sources, delta.Source)
	}
	artifacts := make([]ApprovalArtifact, 0, len(sources))
	seen := map[string]bool{}
	for _, source := range sources {
		relative, err := filepath.Rel(changeRoot, source.Path)
		relative = filepath.ToSlash(relative)
		if err != nil || !source.Present || !safeApprovalPath(relative) || seen[relative] {
			return ApprovalIdentity{}, errors.New("canonical plan contains an unsafe or absent approval artifact")
		}
		seen[relative] = true
		hash := sha256.New()
		hashField(hash, []byte(relative))
		hashField(hash, source.Bytes)
		artifacts = append(artifacts, ApprovalArtifact{
			Path: relative, Hash: hex.EncodeToString(hash.Sum(nil)),
		})
	}
	slices.SortFunc(artifacts, func(a, b ApprovalArtifact) int { return cmp.Compare(a.Path, b.Path) })
	return ApprovalIdentity{Artifacts: artifacts, AggregateHash: aggregateApprovalHash(artifacts)}, nil
}

func recoverableApproval(records []record.Record, change string, revision uint64) (ApprovalRecord, string, bool) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindApproved || item.ExpectedRevision == nil ||
			item.ResultingRevision == nil || *item.ExpectedRevision != revision ||
			*item.ResultingRevision != revision+1 {
			continue
		}
		var approval ApprovalRecord
		if json.Unmarshal(item.Payload, &approval) != nil ||
			validateApprovalEnvelope(item, approval) != nil ||
			approval.Change != change ||
			approval.RevisionBefore != revision {
			return ApprovalRecord{}, "", false
		}
		return approval, item.ID, true
	}
	return ApprovalRecord{}, "", false
}

func persistApprovalState(statePath string, current state.State, approval ApprovalRecord, historyID string) error {
	raw, err := json.Marshal(approval)
	if err != nil {
		return err
	}
	current.Stage = "approved"
	current.Revision++
	current.Approvals = append(current.Approvals, raw)
	current.LastTransition = historyID
	next, err := state.Encode(current)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(statePath, next, nil)
}

func approvalCoveredPaths(changeRoot string, snapshot CheckSnapshot) ([]string, error) {
	sources := []string{
		snapshot.snapshot.Plan.Proposal.Source.Path,
		snapshot.snapshot.Plan.Design.Source.Path,
		snapshot.snapshot.Plan.Tasks.Source.Path,
	}
	for _, delta := range snapshot.snapshot.Plan.Deltas {
		sources = append(sources, delta.Source.Path)
	}
	covered := make([]string, 0, len(sources))
	for _, source := range sources {
		relative, err := filepath.Rel(changeRoot, source)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			return nil, errors.New("planning artifact escapes change root")
		}
		covered = append(covered, filepath.ToSlash(relative))
	}
	return covered, nil
}
