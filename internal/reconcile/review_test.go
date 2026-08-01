package reconcile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const proposalBody = "# Cache work\n\n## Problem\n\nCaching is wrong.\n\n## Outcome\n\nCaching becomes correct.\n\n## Scope\n\nThe cache policy capability.\n\n## Non-goals\n\nNo queue changes.\n\n## Affected capabilities\n\ncache-policy\n"

func readyInput(t *testing.T, owner *corepath.Owner) ReviewInput {
	t.Helper()
	path, err := owner.ChangeProposal(change)
	if err != nil {
		t.Fatalf("proposal path: %v", err)
	}
	write(t, path, proposalBody)
	return ReviewInput{
		Change:              plan.Change{Proposal: plan.ParseProposal(source(t, path))},
		Approval:            ApprovalFact{Current: true},
		Tasks:               TaskFact{Total: 2, Completed: 2},
		Evidence:            EvidenceFact{Current: 2},
		ArchiveTarget:       filepath.Join(owner.Archive(), "2026-07-30-"+change),
		ProjectionAvailable: true,
	}
}

func source(t *testing.T, path string) plan.Source {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return plan.Source{Path: path, Bytes: raw, Present: true}
}

func blockerCodes(review Review) []string {
	found := make([]string, 0, len(review.Blockers))
	for _, item := range review.Blockers {
		if item.Next == "" || item.Owner == "" {
			found = append(found, item.Code+" (no owner or next action)")
			continue
		}
		found = append(found, item.Code)
	}
	return found
}

func TestReviewProjectsReadyPlan(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", fixture(t, "accepted.md"))
	writeDelta(t, owner, "cache-policy", fixture(t, "delta.md"))
	reconciliation := Build(owner, change)
	input := readyInput(t, owner)

	before, err := os.ReadFile(reconciliation.Capabilities[0].AcceptedPath)
	if err != nil {
		t.Fatalf("read accepted: %v", err)
	}
	review := ProjectReview(reconciliation, input)
	after, err := os.ReadFile(reconciliation.Capabilities[0].AcceptedPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("review changed managed bytes: %v", err)
	}

	if !review.Ready || len(review.Blockers) != 0 || review.NoOp || !review.Applicable {
		t.Fatalf("review = %#v", review)
	}
	if review.Outcome != "Caching becomes correct." || review.NonGoals != "No queue changes." {
		t.Fatalf("proposal projection = %q / %q", review.Outcome, review.NonGoals)
	}
	if len(review.Operations) != 4 || review.ArchiveTarget != input.ArchiveTarget {
		t.Fatalf("operations/target = %#v %q", review.Operations, review.ArchiveTarget)
	}
	capability := review.Capabilities[0]
	if capability.BeforeHash == "" || capability.AfterHash == "" || capability.BeforeHash == capability.AfterHash {
		t.Fatalf("hash projection = %#v", capability)
	}
	if !reflect.DeepEqual(review, ProjectReview(Build(owner, change), input)) {
		t.Fatalf("review is not deterministic")
	}
}

func TestReviewProjectsNoOpPlan(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", acceptedOne)
	writeDelta(t, owner, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	review := ProjectReview(Build(owner, change), readyInput(t, owner))
	if !review.NoOp || !review.Ready || len(review.Blockers) != 0 {
		t.Fatalf("no-op review = %#v", review)
	}
	if !review.Capabilities[0].NoOp || review.Capabilities[0].BeforeHash != review.Capabilities[0].AfterHash {
		t.Fatalf("no-op capability = %#v", review.Capabilities[0])
	}
}

func TestReviewLabelsEveryBlockerWithoutAuthorizing(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", acceptedOne)
	// A conflicting delta: MODIFIED naming an identity accepted truth lacks.
	writeDelta(t, owner, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Unknown behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	reconciliation := Build(owner, change)

	input := readyInput(t, owner)
	input.Approval = ApprovalFact{Current: false, Reason: "artifact_changed"}
	input.Tasks = TaskFact{Total: 3, Completed: 1, Incomplete: []string{"T2", "T3"}}
	input.Evidence = EvidenceFact{Current: 1, Stale: []string{"T2"}}
	input.ProjectionAvailable = false
	input.ArchiveTarget = ""

	review := ProjectReview(reconciliation, input)
	want := []string{
		"approval_stale", "archive_target_unavailable", "evidence_stale",
		"plan_not_applicable", "projection_unavailable", "tasks_incomplete",
	}
	if !reflect.DeepEqual(blockerCodes(review), want) {
		t.Fatalf("blockers = %v", blockerCodes(review))
	}
	if review.Ready || review.Applicable || review.NoOp || len(review.Diagnostics) == 0 {
		t.Fatalf("blocked review authorizes work: %#v", review)
	}
	for _, capability := range review.Capabilities {
		if capability.AfterHash != "" {
			t.Fatalf("blocked review exposes an output hash: %#v", capability)
		}
	}
}

func TestReviewHandlesMalformedAndMissingPredecessors(t *testing.T) {
	owner := newOwner(t)
	writeDelta(t, owner, "cache-policy", "## ADDED Requirements\n\n### Requirement: Fresh\n\nno normative text here\n")
	reconciliation := Build(owner, change)
	// No proposal, no approval, no tasks, no evidence, no projection.
	review := ProjectReview(reconciliation, ReviewInput{})
	if review.Ready || review.Outcome != "" || review.NonGoals != "" {
		t.Fatalf("malformed review = %#v", review)
	}
	for _, code := range []string{"approval_stale", "archive_target_unavailable", "plan_not_applicable", "projection_unavailable"} {
		found := false
		for _, blocker := range review.Blockers {
			found = found || blocker.Code == code
		}
		if !found {
			t.Fatalf("missing blocker %s in %v", code, blockerCodes(review))
		}
	}
	if len(review.Operations) != 0 || review.Operations == nil || review.Capabilities == nil {
		t.Fatalf("empty projections must stay empty lists: %#v", review)
	}
}
