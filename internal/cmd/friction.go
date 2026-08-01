package cmd

import (
	"context"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// FrictionResult is the canonical friction outcome: the observation that was
// appended and the D14 eligibility it now contributes to. Eligibility is a
// projection, not a grant — it reports that the root owner may decide, never
// that a deferred domain became available.
type FrictionResult struct {
	Change      string                     `json:"change"`
	TaskID      string                     `json:"task"`
	Domain      string                     `json:"domain"`
	Operation   string                     `json:"operation"`
	Blocker     string                     `json:"blocker"`
	Consequence string                     `json:"consequence"`
	Actor       string                     `json:"actor"`
	Revision    uint64                     `json:"revision"`
	StateHash   string                     `json:"state_hash"`
	EvidenceSet string                     `json:"evidence_set"`
	Eligibility []core.FrictionEligibility `json:"eligibility"`
}

// The handler is bound beside the operation it runs. Dispatch owns the
// registry-driven mapping; nothing here restates operation semantics.
func init() {
	handlers["friction"] = func(_ context.Context, in invocation) (any, error) {
		return Friction(in.root, in.arg("change"), core.FrictionRequest{
			TaskID:           in.arg("task"),
			Operation:        value[string](in, "--blocked-operation"),
			Domain:           value[string](in, "--domain"),
			Consequence:      value[string](in, "--consequence"),
			Actor:            in.actor,
			ExpectedRevision: value[uint64](in, "--revision"),
		})
	}
}

// Friction records one blocked-work observation through the canonical recorder
// and projects the resulting D14 eligibility. It adds no rule of its own: the
// recorder owns identity, staleness, blocked-task, and domain refusals.
func Friction(root, change string, request core.FrictionRequest) (FrictionResult, error) {
	recorded, err := core.RecordFriction(root, change, request)
	if err != nil {
		return FrictionResult{}, err
	}
	eligibility, err := core.ProjectFrictionEligibility(root)
	if err != nil {
		return FrictionResult{}, err
	}
	return FrictionResult{
		Change: recorded.Change, TaskID: recorded.TaskID, Domain: recorded.Domain,
		Operation: recorded.Operation, Blocker: recorded.Blocker,
		Consequence: recorded.Consequence, Actor: recorded.Actor,
		Revision: recorded.ObservedRevision, StateHash: recorded.StateHash,
		EvidenceSet: recorded.EvidenceSet, Eligibility: eligibility,
	}, nil
}
