package gates

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestProductionGatesCompletePlanPasses(t *testing.T) {
	if findings := Production(productionSnapshot(), productionChecks()); len(findings) != 0 {
		t.Fatalf("complete production plan failed: %#v", findings)
	}
	// Every finding must stay actionable and policy-bound.
	broken := productionSnapshot()
	broken.Plan.Design.References = nil
	for _, finding := range Production(broken, productionChecks()) {
		if finding.Gate == "" || finding.Severity != Error ||
			strings.TrimSpace(finding.Problem) == "" || strings.TrimSpace(finding.Repair) == "" {
			t.Fatalf("non-actionable finding: %#v", finding)
		}
	}
}

func TestProductionGatesAddBlockersWithoutSuppressingCore(t *testing.T) {
	registry := planningRegistry(t)
	snapshot := productionSnapshot()
	if findings := registry.Evaluate(snapshot); len(findings) != 0 {
		t.Fatalf("core gates failed on a production-clean plan: %#v", findings)
	}
	// A plan that passes every core gate can still fail production: the design
	// contract is incomplete. Core output is unchanged either way.
	snapshot.Plan.Design.References = nil
	core := registry.Evaluate(snapshot)
	production := Production(snapshot, productionChecks())
	if len(core) != 0 {
		t.Fatalf("production rule leaked into core gates: %#v", core)
	}
	if !hasGate(production, ProductionDesignGate) {
		t.Fatalf("production design gate missing: %#v", production)
	}
	// Core failures remain reported by core gates while production adds its own.
	snapshot.Plan.Tasks.Diagnostics = []plan.Diagnostic{{
		Code: "task_columns", Location: plan.Location{Path: "tasks.md", Line: 3},
		Message: "task row must contain exactly seven columns", Repair: "match the header",
	}}
	if !hasGate(registry.Evaluate(snapshot), TaskGate) ||
		!hasGate(Production(snapshot, productionChecks()), ProductionDesignGate) {
		t.Fatal("production evaluation changed core gate results")
	}
}

func TestProductionGatesRefusePartialPlan(t *testing.T) {
	cases := []struct {
		name   string
		gate   GateID
		mutate func(*Snapshot)
	}{
		{"design does not cite the requirement", ProductionDesignGate, func(s *Snapshot) {
			s.Plan.Design.References = []plan.RequirementReference{
				{Capability: "sample", Requirement: "Other behavior"},
			}
		}},
		{"task cites no requirement", ProductionTraceGate, func(s *Snapshot) {
			s.Plan.Tasks.Tasks[0].References = nil
		}},
		{"requirement has no task with declared files", ProductionTraceGate, func(s *Snapshot) {
			s.Plan.Tasks.Tasks[0].Files = nil
		}},
		{"acceptance names an undeclared path", ProductionAcceptanceGate, func(s *Snapshot) {
			s.Plan.Tasks.Tasks[0].Acceptance = "internal/other/thing.go returns the value"
		}},
		{"policy digest is unavailable", ProductionPolicyGate, func(s *Snapshot) {
			s.PolicyDigest = "  "
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := productionSnapshot()
			testCase.mutate(&snapshot)
			findings := Production(snapshot, productionChecks())
			if !hasGate(findings, testCase.gate) {
				t.Fatalf("missing %s finding: %#v", testCase.gate, findings)
			}
		})
	}
	// Acceptance naming a path a declared file can produce stays reachable, and
	// a prospective output is never read from disk.
	snapshot := productionSnapshot()
	snapshot.Plan.Tasks.Tasks[0].Acceptance = "internal/sample/new_file.go compiles"
	snapshot.Plan.Tasks.Tasks[0].Files = []string{"internal/sample/sample.go"}
	if findings := Production(snapshot, productionChecks()); hasGate(findings, ProductionAcceptanceGate) {
		t.Fatalf("reachable acceptance blocked: %#v", findings)
	}
}

func TestProductionGatesRefuseUnknownCheckDeclarations(t *testing.T) {
	cases := map[string][]evidence.RequiredCheck{
		"none declared": {},
		"unknown class": {{Class: "scan", CheckID: "secrets"}},
		"malformed id":  {{Class: evidence.ClassBuild, CheckID: "Compile Now"}},
		"duplicate": {
			{Class: evidence.ClassBuild, CheckID: "compile"},
			{Class: evidence.ClassBuild, CheckID: "compile"},
		},
		"redeclared test-run": {{Class: evidence.ClassTestRun, CheckID: "task-verify"}},
		"review only":         {{Class: evidence.ClassReview, CheckID: "change-review"}},
	}
	for name, required := range cases {
		t.Run(name, func(t *testing.T) {
			findings := Production(productionSnapshot(), required)
			if !hasGate(findings, ProductionCheckGate) {
				t.Fatalf("declaration accepted: %#v", findings)
			}
		})
	}
}

func productionChecks() []evidence.RequiredCheck {
	return []evidence.RequiredCheck{
		{Class: evidence.ClassBuild, CheckID: "compile"},
		{Class: evidence.ClassLint, CheckID: "go-vet"},
		{Class: evidence.ClassReview, CheckID: "change-review"},
	}
}

func productionSnapshot() Snapshot {
	reference := plan.RequirementReference{Capability: "sample", Requirement: "Stable updates"}
	task := plan.Task{
		ID: "T1", Role: "builder", Files: []string{"internal/sample/sample.go"},
		References: []plan.RequirementReference{reference},
		Verify:     "go test ./...", Acceptance: "concurrent updates serialize", Valid: true,
		Location: plan.Location{Path: "tasks.md", Line: 3, Column: 1},
	}
	return Snapshot{
		Plan: plan.Change{
			Name:     "safe-change",
			Proposal: plan.Proposal{Source: plan.Source{Path: "proposal.md", Present: true}},
			Design: plan.Design{
				Source:     plan.Source{Path: "design.md", Present: true},
				References: []plan.RequirementReference{reference},
			},
			Deltas: []plan.CapabilityDelta{{
				Capability: "sample", Source: plan.Source{Path: "spec.md", Present: true},
			}},
			Tasks: plan.Tasks{
				Source: plan.Source{Path: "tasks.md", Present: true},
				Tasks:  []plan.Task{task},
			},
			Trace: plan.Trace{
				Requirements: []plan.RequirementTrace{{
					Capability: "sample", Requirement: "Stable updates",
					Location: plan.Location{Path: "spec.md", Line: 4}, Tasks: []string{"T1"},
				}},
				Tasks: []plan.TaskTrace{{
					TaskID: "T1", Requirements: []plan.RequirementReference{reference},
				}},
			},
		},
		Lifecycle: "planning", StateRevision: 1,
		Transition: PlanningToApproved, RegistryVersion: CoreRegistryVersion,
		PolicyDigest: strings.Repeat("a", 64),
	}
}
