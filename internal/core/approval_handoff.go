package core

import (
	"fmt"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

const ApprovalAssuranceAdvisory = "advisory"

type ApprovalRoute string

const (
	ApprovalRouteHumanTerminal ApprovalRoute = "human_terminal"
	ApprovalRouteAgentCapable  ApprovalRoute = "agent_capable"
)

type ApprovalOperationFacts struct {
	Operation       string `json:"operation"`
	HumanOnly       bool   `json:"humanOnly"`
	AgentCallable   bool   `json:"agentCallable"`
	CreatesEvidence bool   `json:"createsEvidence"`
	Assurance       string `json:"assurance"`
}

func ApprovalHumanOnlyFacts() ApprovalOperationFacts {
	return ApprovalOperationFacts{
		Operation: "approve", HumanOnly: true, AgentCallable: false,
		CreatesEvidence: false, Assurance: ApprovalAssuranceAdvisory,
	}
}

func (facts ApprovalOperationFacts) AuthorizeAgentCapableRoute() error {
	return failure.New(
		"human_approval_required", "", "",
		"approve is human-only and cannot use an agent-capable route",
		"hand off approval to a human terminal",
	)
}

type ApprovalHandoff struct {
	HumanApprovalRequired bool               `json:"humanApprovalRequired"`
	Gate                  string             `json:"gate"`
	Findings              []gates.Finding    `json:"findings"`
	ReviewedArtifacts     []ApprovalArtifact `json:"reviewedArtifacts"`
	HumanInstruction      string             `json:"humanInstruction"`
	Assurance             string             `json:"assurance"`
}

// ApprovalHandoffFor projects read-only agent handoff data. It never performs
// approval, emits evidence, or treats configuration as host proof.
func ApprovalHandoffFor(root, change string) (*ApprovalHandoff, error) {
	_, handoff, err := ApprovalStatusProjection(root, change)
	return handoff, err
}

// ApprovalStatusProjection reads lifecycle, gates, hashes, and approval
// applicability while holding one change lock.
func ApprovalStatusProjection(root, change string) (state.Projection, *ApprovalHandoff, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return state.Projection{}, nil, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return state.Projection{}, nil, err
	}
	var projection state.Projection
	var handoff *ApprovalHandoff
	err = lock.With(lockPath, func() error {
		statePath, pathErr := owner.ChangeState(change)
		if pathErr != nil {
			return pathErr
		}
		raw, readErr := readCheckState(statePath)
		if readErr != nil {
			return readErr
		}
		current, decodeErr := state.Decode(raw, change)
		if decodeErr != nil {
			return decodeErr
		}
		projection = current.Project()
		snapshot, assembleErr := assembleCheckLocked(owner, change, DefaultPolicyDigest())
		if assembleErr != nil {
			return assembleErr
		}
		handoff, assembleErr = approvalHandoffLocked(owner, snapshot)
		return assembleErr
	})
	return projection, handoff, err
}

func approvalHandoffLocked(owner *corepath.Owner, snapshot CheckSnapshot) (*ApprovalHandoff, error) {
	if snapshot.snapshot.Lifecycle != "planning" {
		status, err := projectApprovalStatusLocked(
			owner, snapshot.change, snapshot.registry.Version(), snapshot.snapshot.PolicyDigest,
		)
		return approvalHandoffForStatus(snapshot.change, status, err)
	}
	result := EvaluateCheck(snapshot)
	handoff := newApprovalHandoff(snapshot.change)
	handoff.Findings = append(handoff.Findings, result.Findings...)
	changeRoot, err := owner.Change(snapshot.change)
	if err != nil {
		return nil, err
	}
	if covered, coverErr := approvalCoveredPaths(changeRoot, snapshot); coverErr != nil {
		appendHandoffHashFinding(handoff, coverErr)
	} else {
		if identity, identityErr := ComputeApprovalIdentity(changeRoot, covered); identityErr == nil {
			handoff.ReviewedArtifacts = append(handoff.ReviewedArtifacts, identity.Artifacts...)
		} else {
			appendHandoffHashFinding(handoff, identityErr)
		}
	}
	if len(handoff.Findings) != 0 {
		handoff.HumanInstruction = fmt.Sprintf(
			"repair findings and run specd check %s, then ask a human to run specd approve %s in a human terminal",
			snapshot.change, snapshot.change,
		)
	}
	return handoff, nil
}

func approvalHandoffForStatus(change string, status ApprovalStatus, err error) (*ApprovalHandoff, error) {
	if err != nil || status.Current {
		return nil, err
	}
	handoff := newApprovalHandoff(change)
	handoff.Findings = append(handoff.Findings, gates.Finding{
		Gate: gates.ApprovalPrerequisiteGate, Severity: gates.Error,
		Problem: status.Reason, Repair: status.Recovery,
	})
	if status.Approval != nil {
		handoff.ReviewedArtifacts = append(handoff.ReviewedArtifacts, status.Approval.Artifacts...)
	}
	handoff.HumanInstruction = status.Recovery
	return handoff, nil
}

func newApprovalHandoff(change string) *ApprovalHandoff {
	return &ApprovalHandoff{
		HumanApprovalRequired: true,
		Gate:                  "planning_to_approved",
		Findings:              []gates.Finding{},
		ReviewedArtifacts:     []ApprovalArtifact{},
		HumanInstruction:      fmt.Sprintf("ask a human to run specd approve %s in a human terminal", change),
		Assurance:             ApprovalAssuranceAdvisory,
	}
}

func appendHandoffHashFinding(handoff *ApprovalHandoff, err error) {
	handoff.Findings = append(handoff.Findings, gates.Finding{
		Gate: gates.ApprovalPrerequisiteGate, Severity: gates.Error,
		Problem: "reviewed artifact hashes are unavailable: " + err.Error(),
		Repair:  "repair planning artifacts and run check before human approval",
	})
}
