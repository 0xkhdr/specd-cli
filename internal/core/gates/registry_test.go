package gates

import (
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestRegistryCoreCompletenessAndOrder(t *testing.T) {
	evaluators := coreEvaluators(func(Snapshot) []Issue { return nil })
	registry, err := CoreRegistry(evaluators)
	if err != nil {
		t.Fatal(err)
	}
	want := []GateID{
		ProposalGate, DeltaGate, DesignGate, TaskGate, TraceGate,
		VerificationGate, ApprovalPrerequisiteGate, LifecycleGate,
	}
	var got []GateID
	for _, gate := range registry.Gates() {
		got = append(got, gate.ID)
		if gate.Description == "" || gate.Transition != PlanningToApproved ||
			gate.Profile != DefaultProfile || len(gate.Artifacts) == 0 ||
			gate.Severity == "" || gate.Evaluate == nil {
			t.Fatalf("incomplete gate: %#v", gate)
		}
	}
	if !reflect.DeepEqual(got, want) || registry.Version() != CoreRegistryVersion {
		t.Fatalf("registry drift: %v %q", got, registry.Version())
	}
}

func TestRegistryRejectsInvalidMetadata(t *testing.T) {
	valid := Gate{
		ID: "one", Description: "one gate", Transition: PlanningToApproved,
		Profile: DefaultProfile, Artifacts: []Artifact{Proposal}, Severity: Error,
		Evaluate: func(Snapshot) []Issue { return nil },
	}
	tests := []struct {
		name        string
		gates       []Gate
		transitions []Transition
		want        string
	}{
		{"incomplete", []Gate{{ID: "one"}}, nil, "metadata is incomplete"},
		{"duplicate gate", []Gate{valid, valid}, nil, "appears more than once"},
		{"unknown gate", []Gate{valid}, []Transition{{ID: PlanningToApproved, GateIDs: []GateID{"missing"}}}, "unregistered gate"},
		{"duplicate transition gate", []Gate{valid}, []Transition{{ID: PlanningToApproved, GateIDs: []GateID{"one", "one"}}}, "repeats gate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newRegistry("v1", test.gates, test.transitions)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "next:") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestGatePurityDeterministicValues(t *testing.T) {
	first := func(snapshot Snapshot) []Issue {
		return []Issue{
			{Location: plan.Location{Path: "b.md", Line: 2}, Problem: snapshot.PolicyDigest, Repair: "repair b"},
			{Location: plan.Location{Path: "a.md", Line: 3}, Problem: "later", Repair: "repair a"},
			{Location: plan.Location{Path: "a.md", Line: 1}, Problem: "first", Repair: "repair a"},
		}
	}
	second := func(Snapshot) []Issue {
		return []Issue{{Location: plan.Location{Path: "a.md", Line: 1}, Problem: "second gate", Repair: "repair"}}
	}
	registry, err := newRegistry("v1", []Gate{
		{ID: "first", Description: "first", Transition: PlanningToApproved, Profile: DefaultProfile, Artifacts: []Artifact{Proposal}, Severity: Error, Evaluate: first},
		{ID: "second", Description: "second", Transition: PlanningToApproved, Profile: DefaultProfile, Artifacts: []Artifact{Design}, Severity: Warning, Evaluate: second},
	}, []Transition{{ID: PlanningToApproved, GateIDs: []GateID{"first", "second"}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Transition: PlanningToApproved, RegistryVersion: "v1", PolicyDigest: "same values"}
	a, b := registry.Evaluate(snapshot), registry.Evaluate(snapshot)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same snapshot changed findings: %#v != %#v", a, b)
	}
	if got := []string{a[0].Location.Path, a[1].Location.Path, a[2].Location.Path}; !reflect.DeepEqual(got, []string{"a.md", "a.md", "b.md"}) {
		t.Fatalf("findings not sorted inside registry order: %v", got)
	}
	if a[3].Gate != "second" || a[3].Severity != Warning {
		t.Fatalf("registry metadata not projected: %#v", a[3])
	}
}

func TestRegistryCanonicalOrderAndSnapshotIsolation(t *testing.T) {
	first := func(snapshot Snapshot) []Issue {
		snapshot.Plan.Name = "mutated"
		snapshot.Plan.Proposal.Source.Bytes[0] = 'x'
		return []Issue{{Problem: "first", Repair: "repair first"}}
	}
	second := func(snapshot Snapshot) []Issue {
		if snapshot.Plan.Name != "original" || string(snapshot.Plan.Proposal.Source.Bytes) != "safe" {
			return []Issue{{Problem: "snapshot was mutated", Repair: "isolate snapshots"}}
		}
		return []Issue{{Problem: "second", Repair: "repair second"}}
	}
	registry, err := newRegistry("v1", []Gate{
		{ID: "first", Description: "first", Transition: PlanningToApproved, Profile: DefaultProfile, Artifacts: []Artifact{Proposal}, Severity: Error, Evaluate: first},
		{ID: "second", Description: "second", Transition: PlanningToApproved, Profile: DefaultProfile, Artifacts: []Artifact{Design}, Severity: Error, Evaluate: second},
	}, []Transition{{ID: PlanningToApproved, GateIDs: []GateID{"second", "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{
		Plan:       plan.Change{Name: "original", Proposal: plan.Proposal{Source: plan.Source{Bytes: []byte("safe")}}},
		Transition: PlanningToApproved, RegistryVersion: "v1",
	}
	findings := registry.Evaluate(snapshot)
	if len(findings) != 2 || findings[0].Gate != "first" || findings[1].Gate != "second" ||
		snapshot.Plan.Name != "original" || string(snapshot.Plan.Proposal.Source.Bytes) != "safe" {
		t.Fatalf("order/isolation failed: %#v %#v", findings, snapshot)
	}
}

func TestRegistryFailClosedControlFindings(t *testing.T) {
	valid := Gate{
		ID: "one", Description: "one", Transition: PlanningToApproved,
		Profile: DefaultProfile, Artifacts: []Artifact{Proposal}, Severity: Error,
		Evaluate: func(Snapshot) []Issue { return []Issue{{}} },
	}
	registry, err := newRegistry("v1", []Gate{valid}, []Transition{{ID: PlanningToApproved, GateIDs: []GateID{"one"}}})
	if err != nil {
		t.Fatal(err)
	}
	findings := registry.Evaluate(Snapshot{Transition: PlanningToApproved, RegistryVersion: "old"})
	if len(findings) != 2 || findings[0].Gate != RegistryGate || findings[1].Gate != RegistryGate {
		t.Fatalf("version/actionability not refused: %#v", findings)
	}
	findings = registry.Evaluate(Snapshot{Transition: "unknown", RegistryVersion: "v1"})
	if len(findings) != 1 || findings[0].Gate != RegistryGate ||
		findings[0].Problem == "" || findings[0].Repair == "" {
		t.Fatalf("unknown transition not typed: %#v", findings)
	}
}

func TestGatePurityEvaluatorInventory(t *testing.T) {
	evaluators := coreEvaluators(func(Snapshot) []Issue { return nil })
	delete(evaluators, ProposalGate)
	if _, err := CoreRegistry(evaluators); err == nil || !strings.Contains(err.Error(), "next:") {
		t.Fatalf("missing evaluator accepted: %v", err)
	}
	evaluators = coreEvaluators(func(Snapshot) []Issue { return nil })
	evaluators["network_gate"] = func(Snapshot) []Issue { return nil }
	if _, err := CoreRegistry(evaluators); err == nil {
		t.Fatal("external evaluator inventory accepted")
	}
}

func coreEvaluators(evaluator Evaluator) map[GateID]Evaluator {
	return map[GateID]Evaluator{
		ProposalGate: evaluator, DeltaGate: evaluator, DesignGate: evaluator,
		TaskGate: evaluator, TraceGate: evaluator,
		VerificationGate: evaluator, ApprovalPrerequisiteGate: evaluator,
		LifecycleGate: evaluator,
	}
}
