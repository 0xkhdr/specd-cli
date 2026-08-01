package gates

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestPlanningGatesComplete(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	if findings := registry.Evaluate(snapshot); len(findings) != 0 {
		t.Fatalf("complete plan failed: %#v", findings)
	}
}

func TestPlanningGatesPartialAccumulates(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	snapshot.Plan.Proposal.Diagnostics = []plan.Diagnostic{{
		Code: "section_missing", Location: plan.Location{Path: "proposal.md", Line: 1},
		Message: "Problem section is missing", Repair: "add ## Problem",
	}}
	snapshot.Plan.Tasks.Diagnostics = []plan.Diagnostic{{
		Code: "task_dependency_cycle", Location: plan.Location{Path: "tasks.md", Line: 3},
		Message: "task dependency cycle", Repair: "remove one dependency",
	}}
	snapshot.Plan.Diagnostics = []plan.Diagnostic{
		snapshot.Plan.Proposal.Diagnostics[0], snapshot.Plan.Tasks.Diagnostics[0],
		{Code: "trace_unknown", Location: plan.Location{Path: "tasks.md", Line: 3},
			Message: "unknown requirement", Repair: "cite one requirement"},
	}
	findings := registry.Evaluate(snapshot)
	for _, id := range []GateID{ProposalGate, TaskGate, TraceGate} {
		if !hasGate(findings, id) {
			t.Fatalf("missing independent %s finding: %#v", id, findings)
		}
	}
}

func TestPlanningGatesProspectiveOutput(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	// Declared task files that do not exist yet are prospective outputs. No gate
	// reads the filesystem, so their absence can never block approval.
	snapshot.Plan.Tasks.Tasks[0].Files = []string{"new/file.go"}
	if findings := registry.Evaluate(snapshot); len(findings) != 0 {
		t.Fatalf("prospective output blocked: %#v", findings)
	}
}

func TestPlanningGatesVerification(t *testing.T) {
	registry := planningRegistry(t)
	for _, command := range []string{
		"", "go test ./... && other", "go test 'unterminated", "GOFLAGS=-x", "# comment",
		"go test $(touch marker)", "go test `touch marker`", "go test (subshell)",
	} {
		snapshot := greenSnapshot()
		snapshot.Plan.Tasks.Tasks[0].Verify = command
		findings := registry.Evaluate(snapshot)
		if !hasGate(findings, VerificationGate) {
			t.Fatalf("invalid command %q passed: %#v", command, findings)
		}
	}
	snapshot := greenSnapshot()
	snapshot.Plan.Tasks.Tasks[0].Verify = `go test ./internal/plan -run 'TestParse|TestTrace'`
	if findings := registry.Evaluate(snapshot); hasGate(findings, VerificationGate) {
		t.Fatalf("quoted command failed: %#v", findings)
	}
	snapshot = greenSnapshot()
	snapshot.Plan.Tasks.Tasks[0].Valid = false
	snapshot.Plan.Tasks.Tasks[0].Verify = "go test ./... && other"
	if findings := registry.Evaluate(snapshot); !hasGate(findings, VerificationGate) {
		t.Fatalf("invalid task hid independent verification failure: %#v", findings)
	}
	// A scaffold placeholder is reported once, by the parser diagnostic that
	// names the real repair, and never as broken shell syntax.
	snapshot = greenSnapshot()
	snapshot.Plan.Tasks.Tasks[0].Verify = "<verification>"
	snapshot.Plan.Diagnostics = append(snapshot.Plan.Diagnostics, plan.Diagnostic{
		Code:     "task_verify_placeholder",
		Location: snapshot.Plan.Tasks.Tasks[0].Location,
		Message:  "task verification command contains placeholder text",
		Repair:   "replace it with one exact verification command",
	})
	if findings := registry.Evaluate(snapshot); hasGate(findings, VerificationGate) {
		t.Fatalf("placeholder reported as command syntax: %#v", findings)
	}
}

func TestPlanningGatesDeltaLocalDiagnosticsDeduplicate(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	diagnostic := plan.Diagnostic{
		Code: "scenario_missing", Location: plan.Location{Path: "spec.md", Line: 3},
		Message: "requirement has no scenario", Repair: "add one scenario",
	}
	snapshot.Plan.Deltas[0].Diagnostics = []plan.Diagnostic{diagnostic}
	if findings := registry.Evaluate(snapshot); countGate(findings, DeltaGate) != 1 {
		t.Fatalf("delta-local diagnostic missing: %#v", findings)
	}
	snapshot.Plan.Diagnostics = []plan.Diagnostic{diagnostic}
	if findings := registry.Evaluate(snapshot); countGate(findings, DeltaGate) != 1 {
		t.Fatalf("aggregated delta diagnostic duplicated: %#v", findings)
	}
}

func TestPlanningGatesDesignReferenceOwnedByDesignGate(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	diagnostic := plan.Diagnostic{
		Code: "reference_syntax", Location: plan.Location{Path: "design.md", Line: 3},
		Message: "invalid requirement reference", Repair: "use capability/Requirement: name",
	}
	snapshot.Plan.Design.Diagnostics = []plan.Diagnostic{diagnostic}
	snapshot.Plan.Diagnostics = []plan.Diagnostic{diagnostic}
	findings := registry.Evaluate(snapshot)
	if countGate(findings, DesignGate) != 1 || hasGate(findings, ApprovalPrerequisiteGate) {
		t.Fatalf("design reference diagnostic has wrong owner: %#v", findings)
	}
}

func TestPlanningGatesPrerequisiteAndLifecycle(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	snapshot.StateRevision = 0
	snapshot.RegistryVersion = "old"
	snapshot.PolicyDigest = ""
	snapshot.Lifecycle = "approved"
	findings := registry.Evaluate(snapshot)
	if countGate(findings, ApprovalPrerequisiteGate) != 2 || !hasGate(findings, LifecycleGate) {
		t.Fatalf("invalid approval snapshot passed: %#v", findings)
	}
	for _, finding := range findings {
		if finding.Problem == "" || finding.Repair == "" {
			t.Fatalf("non-actionable finding: %#v", finding)
		}
	}
}

func TestPlanningGatesMalformedCanonicalInput(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := greenSnapshot()
	snapshot.Plan.Diagnostics = []plan.Diagnostic{{
		Code: "change_unsafe", Location: plan.Location{Path: "../escape", Line: 1},
		Message: "change path is unsafe", Repair: "choose a safe change",
	}}
	findings := registry.Evaluate(snapshot)
	if !hasGate(findings, ApprovalPrerequisiteGate) ||
		!strings.Contains(findings[0].Repair+findings[len(findings)-1].Repair, "safe") {
		t.Fatalf("unsafe canonical input passed: %#v", findings)
	}
}

func planningRegistry(t *testing.T) Registry {
	t.Helper()
	registry, err := PlanningRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func greenSnapshot() Snapshot {
	task := plan.Task{
		ID: "T1", Role: "builder", Files: []string{"internal/example.go"},
		Verify: "go test ./...", Acceptance: "check passes", Valid: true,
		Location: plan.Location{Path: "tasks.md", Line: 3, Column: 1},
	}
	return Snapshot{
		Plan: plan.Change{
			Name:     "complete",
			Proposal: plan.Proposal{Source: plan.Source{Path: "proposal.md", Present: true}},
			Design:   plan.Design{Source: plan.Source{Path: "design.md", Present: true}},
			Deltas: []plan.CapabilityDelta{{
				Capability: "example", Source: plan.Source{Path: "spec.md", Present: true},
			}},
			Tasks: plan.Tasks{
				Source: plan.Source{Path: "tasks.md", Present: true},
				Tasks:  []plan.Task{task},
			},
		},
		Lifecycle: "planning", StateRevision: 1,
		Transition: PlanningToApproved, RegistryVersion: CoreRegistryVersion,
		PolicyDigest: "default-policy",
	}
}

func hasGate(findings []Finding, id GateID) bool { return countGate(findings, id) > 0 }

func countGate(findings []Finding, id GateID) int {
	count := 0
	for _, finding := range findings {
		if finding.Gate == id {
			count++
		}
	}
	return count
}
