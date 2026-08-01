package reconcile

import (
	"sort"
	"strconv"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

// ApprovalFact, TaskFact, and EvidenceFact are the predecessor projections
// review consumes. They are injected as values so review stays pure and so
// approval, activity, readiness, evidence, and accepted truth remain distinct
// concepts rather than one fused "ready" flag.
type ApprovalFact struct {
	Current bool   `json:"current"`
	Reason  string `json:"reason,omitempty"`
}

type TaskFact struct {
	Total      int      `json:"total"`
	Completed  int      `json:"completed"`
	Incomplete []string `json:"incomplete,omitempty"`
}

type EvidenceFact struct {
	Current int      `json:"current"`
	Stale   []string `json:"stale,omitempty"`
}

// ReviewInput carries everything review may not read for itself.
type ReviewInput struct {
	Change              plan.Change
	Approval            ApprovalFact
	Tasks               TaskFact
	Evidence            EvidenceFact
	ArchiveTarget       string
	ProjectionAvailable bool
}

// Blocker is one labelled reason this change may not sync or archive yet. It is
// never authorization and never a completion claim.
type Blocker struct {
	Code   string `json:"code"`
	Owner  string `json:"owner"`
	Reason string `json:"reason"`
	Next   string `json:"next"`
}

// ReviewCapability is the before/after projection of one capability.
type ReviewCapability struct {
	Capability   string `json:"capability"`
	AcceptedPath string `json:"accepted_path"`
	DeltaPath    string `json:"delta_path"`
	BeforeHash   string `json:"before_hash,omitempty"`
	AfterHash    string `json:"after_hash,omitempty"`
	Created      bool   `json:"created"`
	NoOp         bool   `json:"no_op"`
}

// Review is the read-only projection shown before human authorization.
type Review struct {
	Change        string             `json:"change"`
	Root          string             `json:"root"`
	Outcome       string             `json:"outcome,omitempty"`
	NonGoals      string             `json:"non_goals,omitempty"`
	Operations    []Operation        `json:"operations"`
	Capabilities  []ReviewCapability `json:"capabilities"`
	Tasks         TaskFact           `json:"tasks"`
	Evidence      EvidenceFact       `json:"evidence"`
	Approval      ApprovalFact       `json:"approval"`
	ArchiveTarget string             `json:"archive_target,omitempty"`
	Diagnostics   []plan.Diagnostic  `json:"diagnostics,omitempty"`
	Blockers      []Blocker          `json:"blockers,omitempty"`
	Applicable    bool               `json:"applicable"`
	NoOp          bool               `json:"no_op"`
	Ready         bool               `json:"ready"`
}

// ProjectReview renders one reconciliation plan and its predecessor facts.
// It reads nothing, writes nothing, and authorizes nothing: Ready means no
// blocker was found, never that sync may proceed without a human.
func ProjectReview(reconciliation Plan, input ReviewInput) Review {
	review := Review{
		Change: reconciliation.Change, Root: reconciliation.Root,
		Outcome:    section(input.Change.Proposal, "Outcome"),
		NonGoals:   section(input.Change.Proposal, "Non-goals"),
		Operations: []Operation{}, Capabilities: []ReviewCapability{},
		Tasks: input.Tasks, Evidence: input.Evidence, Approval: input.Approval,
		ArchiveTarget: input.ArchiveTarget, Diagnostics: reconciliation.Diagnostics,
		Applicable: reconciliation.Applicable, NoOp: reconciliation.NoOp,
	}
	for _, capability := range reconciliation.Capabilities {
		review.Operations = append(review.Operations, capability.Operations...)
		review.Capabilities = append(review.Capabilities, ReviewCapability{
			Capability: capability.Capability, AcceptedPath: capability.AcceptedPath,
			DeltaPath: capability.DeltaPath, BeforeHash: capability.AcceptedHash,
			AfterHash: capability.OutputHash, Created: capability.Created,
			NoOp: capability.NoOp,
		})
	}

	if !reconciliation.Applicable {
		reason := "reconciliation produced no applicable plan"
		if count := len(reconciliation.Diagnostics); count != 0 {
			reason = "reconciliation reported " + strconv.Itoa(count) + " blocking diagnostic(s)"
		}
		review.Blockers = append(review.Blockers, Blocker{
			Code: "plan_not_applicable", Owner: "author", Reason: reason,
			Next: "repair the reported delta or accepted spec and run specd check " + reconciliation.Change,
		})
	}
	if input.Tasks.Completed < input.Tasks.Total || len(input.Tasks.Incomplete) != 0 {
		review.Blockers = append(review.Blockers, Blocker{
			Code: "tasks_incomplete", Owner: "author",
			Reason: strconv.Itoa(input.Tasks.Total-input.Tasks.Completed) + " task(s) are not complete",
			Next:   "run specd next " + reconciliation.Change + " and complete the remaining tasks",
		})
	}
	if len(input.Evidence.Stale) != 0 {
		review.Blockers = append(review.Blockers, Blocker{
			Code: "evidence_stale", Owner: "author",
			Reason: "evidence is stale for " + strings.Join(input.Evidence.Stale, ", "),
			Next:   "re-verify the affected tasks against current HEAD",
		})
	}
	if !input.Approval.Current {
		reason := "change approval is not current"
		if input.Approval.Reason != "" {
			reason = "change approval is not current: " + input.Approval.Reason
		}
		review.Blockers = append(review.Blockers, Blocker{
			Code: "approval_stale", Owner: "human", Reason: reason,
			Next: "ask a human to run specd approve " + reconciliation.Change + " in a human terminal",
		})
	}
	if !input.ProjectionAvailable {
		review.Blockers = append(review.Blockers, Blocker{
			Code: "projection_unavailable", Owner: "author",
			Reason: "the canonical operation projection is unavailable",
			Next:   "run specd status " + reconciliation.Change + " to observe canonical state",
		})
	}
	if input.ArchiveTarget == "" {
		review.Blockers = append(review.Blockers, Blocker{
			Code: "archive_target_unavailable", Owner: "author",
			Reason: "no archive target was resolved for this change",
			Next:   "run specd status " + reconciliation.Change + " to observe canonical state",
		})
	}
	sort.SliceStable(review.Blockers, func(i, j int) bool {
		return review.Blockers[i].Code < review.Blockers[j].Code
	})
	review.Ready = len(review.Blockers) == 0
	return review
}

func section(proposal plan.Proposal, name string) string {
	for _, item := range proposal.Sections {
		if item.Name == name {
			return strings.TrimSpace(string(item.Content))
		}
	}
	return ""
}
