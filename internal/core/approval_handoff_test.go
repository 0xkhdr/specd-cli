package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestApprovalHumanOnlyFactsAndRoute(t *testing.T) {
	facts := ApprovalHumanOnlyFacts()
	if facts.Operation != "approve" || !facts.HumanOnly || facts.AgentCallable ||
		facts.CreatesEvidence || facts.Assurance != ApprovalAssuranceAdvisory {
		t.Fatalf("approval facts = %#v", facts)
	}
	err := facts.AuthorizeAgentCapableRoute()
	if !failure.IsCode(err, "human_approval_required") ||
		!strings.Contains(err.Error(), "human terminal") {
		t.Fatalf("agent route refusal = %v", err)
	}
}

func TestApprovalHumanOnlyCreatesNoEvidence(t *testing.T) {
	root := checkRoot(t, true)
	evidence := filepath.Join(root, ".specd", "evidence.jsonl")
	before, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(root, "safe-change", ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(evidence)
	if err != nil || string(after) != string(before) {
		t.Fatalf("approval changed evidence: %v", err)
	}
}

func TestApprovalHandoffExactReviewedFacts(t *testing.T) {
	root := checkRoot(t, true)
	handoff, err := ApprovalHandoffFor(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil || !handoff.HumanApprovalRequired ||
		handoff.Gate != "planning_to_approved" ||
		handoff.Assurance != ApprovalAssuranceAdvisory ||
		len(handoff.Findings) != 0 || len(handoff.ReviewedArtifacts) != 4 ||
		!strings.Contains(handoff.HumanInstruction, "human terminal") {
		t.Fatalf("handoff = %#v", handoff)
	}
	raw, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"humanApprovalRequired", "gate", "findings", "reviewedArtifacts",
		"humanInstruction", `"assurance":"advisory"`,
	} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("handoff JSON missing %q: %s", field, raw)
		}
	}
}

func TestApprovalHandoffFindingsAndApprovedAbsence(t *testing.T) {
	root := checkRoot(t, false)
	handoff, err := ApprovalHandoffFor(root, "safe-change")
	if err != nil || handoff == nil || len(handoff.Findings) == 0 ||
		!strings.Contains(handoff.HumanInstruction, "specd check safe-change") {
		t.Fatalf("invalid-plan handoff = %#v, %v", handoff, err)
	}
	foundHashFailure := false
	for _, finding := range handoff.Findings {
		foundHashFailure = foundHashFailure || strings.Contains(finding.Problem, "reviewed artifact hashes are unavailable")
	}
	if !foundHashFailure {
		t.Fatalf("hash failure was not actionable: %#v", handoff.Findings)
	}

	root = checkRoot(t, true)
	if _, err := Approve(root, "safe-change", ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	}); err != nil {
		t.Fatal(err)
	}
	if handoff, err := ApprovalHandoffFor(root, "safe-change"); err != nil || handoff != nil {
		t.Fatalf("approved handoff = %#v, %v", handoff, err)
	}
	proposal := filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md")
	file, err := os.OpenFile(proposal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\ndrift\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	handoff, err = ApprovalHandoffFor(root, "safe-change")
	if err != nil || handoff == nil || !handoff.HumanApprovalRequired ||
		len(handoff.Findings) != 1 || handoff.Findings[0].Problem != "artifact_changed" {
		t.Fatalf("stale approved handoff = %#v, %v", handoff, err)
	}
}
