package gates

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

type GateID string
type Profile string
type Severity string
type Artifact string
type TransitionID string

const (
	DefaultProfile Profile = "default"

	Error   Severity = "error"
	Warning Severity = "warning"

	Proposal Artifact = "proposal"
	Deltas   Artifact = "deltas"
	Design   Artifact = "design"
	Tasks    Artifact = "tasks"
	State    Artifact = "state"

	PlanningToApproved TransitionID = "planning_to_approved"
)

const (
	ProposalGate             GateID = "proposal"
	DeltaGate                GateID = "deltas"
	DesignGate               GateID = "design"
	TaskGate                 GateID = "tasks"
	TraceGate                GateID = "trace"
	VerificationGate         GateID = "verification"
	ApprovalPrerequisiteGate GateID = "approval_prerequisite"
	LifecycleGate            GateID = "lifecycle"
	RegistryGate             GateID = "gate_registry"
)

// CoreRegistryVersion changes whenever the gate inventory changes, so any
// approval bound to an older inventory is stale rather than silently reused.
const CoreRegistryVersion = "core-v2"

// Snapshot contains values assembled before gate evaluation. It deliberately
// exposes no filesystem, clock, mutation, network, or LLM capability.
type Snapshot struct {
	Plan            plan.Change
	Lifecycle       string
	StateRevision   uint64
	Transition      TransitionID
	RegistryVersion string
	PolicyDigest    string
}

type Issue struct {
	Location plan.Location
	Problem  string
	Repair   string
}

type Finding struct {
	Gate     GateID
	Severity Severity
	Location plan.Location
	Problem  string
	Repair   string
}

type Evaluator func(Snapshot) []Issue

type Gate struct {
	ID          GateID
	Description string
	Transition  TransitionID
	Profile     Profile
	Artifacts   []Artifact
	Severity    Severity
	Evaluate    Evaluator
}

type Transition struct {
	ID      TransitionID
	GateIDs []GateID
}

type Registry struct {
	version     string
	gates       []Gate
	byID        map[GateID]int
	transitions map[TransitionID][]GateID
	control     Gate
}

func newRegistry(version string, gates []Gate, transitions []Transition) (Registry, error) {
	if strings.TrimSpace(version) == "" {
		return Registry{}, errors.New("gate_registry_invalid: version is required; next: repair gate registry metadata")
	}
	registry := Registry{
		version: version, gates: slices.Clone(gates),
		byID:        make(map[GateID]int, len(gates)),
		transitions: make(map[TransitionID][]GateID, len(transitions)),
		control: Gate{
			ID: RegistryGate, Description: "gate registry input is valid",
			Profile: DefaultProfile, Severity: Error,
		},
	}
	for i := range registry.gates {
		registry.gates[i].Artifacts = slices.Clone(registry.gates[i].Artifacts)
		gate := registry.gates[i]
		if err := validateGate(gate); err != nil {
			return Registry{}, err
		}
		if _, exists := registry.byID[gate.ID]; exists {
			return Registry{}, fmt.Errorf("gate_registry_duplicate: gate %q appears more than once; next: keep one gate definition", gate.ID)
		}
		registry.byID[gate.ID] = i
	}
	for _, transition := range transitions {
		if transition.ID == "" || len(transition.GateIDs) == 0 {
			return Registry{}, errors.New("gate_registry_invalid: transition id and gates are required; next: repair gate registry metadata")
		}
		if _, exists := registry.transitions[transition.ID]; exists {
			return Registry{}, fmt.Errorf("gate_registry_duplicate: transition %q appears more than once; next: keep one transition definition", transition.ID)
		}
		seen := map[GateID]bool{}
		for _, id := range transition.GateIDs {
			index, exists := registry.byID[id]
			if !exists {
				return Registry{}, fmt.Errorf("gate_registry_unknown: transition %q names unregistered gate %q; next: register the gate or remove the reference", transition.ID, id)
			}
			if seen[id] {
				return Registry{}, fmt.Errorf("gate_registry_duplicate: transition %q repeats gate %q; next: keep one gate reference", transition.ID, id)
			}
			if registry.gates[index].Transition != transition.ID {
				return Registry{}, fmt.Errorf("gate_registry_invalid: gate %q transition metadata disagrees with %q; next: repair gate registry metadata", id, transition.ID)
			}
			seen[id] = true
		}
		registry.transitions[transition.ID] = slices.Clone(transition.GateIDs)
	}
	for _, gate := range registry.gates {
		ids, exists := registry.transitions[gate.Transition]
		if !exists || !slices.Contains(ids, gate.ID) {
			return Registry{}, fmt.Errorf("gate_registry_invalid: gate %q is not bound to transition %q; next: repair gate registry metadata", gate.ID, gate.Transition)
		}
	}
	return registry, nil
}

func (registry Registry) Version() string { return registry.version }

func (registry Registry) Gates() []Gate {
	result := slices.Clone(registry.gates)
	for i := range result {
		result[i].Artifacts = slices.Clone(result[i].Artifacts)
	}
	return result
}

func (registry Registry) Evaluate(snapshot Snapshot) []Finding {
	ids, exists := registry.transitions[snapshot.Transition]
	if !exists {
		return []Finding{registry.refusal(
			"transition has no registered gate set",
			"select a registered lifecycle transition",
		)}
	}
	var findings []Finding
	if snapshot.RegistryVersion != registry.version {
		findings = append(findings, registry.refusal(
			"snapshot registry version does not match active registry",
			"reassemble the snapshot with registry version "+registry.version,
		))
	}
	selected := make(map[GateID]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	for _, gate := range registry.gates {
		if !selected[gate.ID] {
			continue
		}
		input, err := cloneSnapshot(snapshot)
		if err != nil {
			findings = append(findings, registry.refusal(
				"snapshot cannot be isolated for gate "+string(gate.ID),
				"repair the canonical snapshot and rerun check",
			))
			continue
		}
		issues := append([]Issue(nil), gate.Evaluate(input)...)
		slices.SortStableFunc(issues, func(a, b Issue) int {
			return cmp.Or(
				cmp.Compare(a.Location.Path, b.Location.Path),
				cmp.Compare(a.Location.Offset, b.Location.Offset),
				cmp.Compare(a.Location.Line, b.Location.Line),
				cmp.Compare(a.Location.Column, b.Location.Column),
				cmp.Compare(a.Problem, b.Problem),
				cmp.Compare(a.Repair, b.Repair),
			)
		})
		for _, issue := range issues {
			if strings.TrimSpace(issue.Problem) == "" || strings.TrimSpace(issue.Repair) == "" {
				findings = append(findings, registry.refusal(
					"gate "+string(gate.ID)+" returned a non-actionable issue",
					"repair the gate evaluator to return one problem and one repair",
				))
				continue
			}
			findings = append(findings, Finding{
				Gate: gate.ID, Severity: gate.Severity, Location: issue.Location,
				Problem: issue.Problem, Repair: issue.Repair,
			})
		}
	}
	return findings
}

func (registry Registry) refusal(problem, repair string) Finding {
	return Finding{
		Gate: registry.control.ID, Severity: registry.control.Severity,
		Problem: problem, Repair: repair,
	}
}

func cloneSnapshot(snapshot Snapshot) (Snapshot, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	var clone Snapshot
	if err := json.Unmarshal(raw, &clone); err != nil {
		return Snapshot{}, err
	}
	return clone, nil
}

func CoreRegistry(evaluators map[GateID]Evaluator) (Registry, error) {
	definitions := []struct {
		id          GateID
		description string
		artifacts   []Artifact
	}{
		{ProposalGate, "proposal is complete", []Artifact{Proposal}},
		{DeltaGate, "capability deltas are structurally valid", []Artifact{Deltas}},
		{DesignGate, "design is complete and references resolve", []Artifact{Design, Deltas}},
		{TaskGate, "task table and dependency graph are valid", []Artifact{Tasks}},
		{TraceGate, "requirements and tasks trace both directions", []Artifact{Deltas, Tasks}},
		{VerificationGate, "verification commands are structurally executable", []Artifact{Tasks}},
		{ApprovalPrerequisiteGate, "approval prerequisites are satisfied", []Artifact{Proposal, Deltas, Design, Tasks, State}},
		{LifecycleGate, "lifecycle transition is legal", []Artifact{State}},
	}
	gates := make([]Gate, 0, len(definitions))
	ids := make([]GateID, 0, len(definitions))
	for _, definition := range definitions {
		evaluator := evaluators[definition.id]
		gates = append(gates, Gate{
			ID: definition.id, Description: definition.description,
			Transition: PlanningToApproved, Profile: DefaultProfile,
			Artifacts: definition.artifacts, Severity: Error, Evaluate: evaluator,
		})
		ids = append(ids, definition.id)
	}
	if len(evaluators) != len(definitions) {
		return Registry{}, errors.New("gate_registry_invalid: evaluator inventory differs from core gates; next: register exactly the core gate evaluators")
	}
	return newRegistry(CoreRegistryVersion, gates, []Transition{{ID: PlanningToApproved, GateIDs: ids}})
}

func validateGate(gate Gate) error {
	if gate.ID == "" || strings.TrimSpace(gate.Description) == "" || gate.Transition == "" ||
		gate.Profile == "" || len(gate.Artifacts) == 0 || gate.Evaluate == nil {
		return fmt.Errorf("gate_registry_invalid: gate %q metadata is incomplete; next: complete every gate metadata field", gate.ID)
	}
	if gate.Profile != DefaultProfile {
		return fmt.Errorf("gate_registry_invalid: gate %q uses unsupported profile %q; next: use the default profile", gate.ID, gate.Profile)
	}
	if gate.Severity != Error && gate.Severity != Warning {
		return fmt.Errorf("gate_registry_invalid: gate %q has invalid severity %q; next: use error or warning", gate.ID, gate.Severity)
	}
	seen := map[Artifact]bool{}
	for _, artifact := range gate.Artifacts {
		switch artifact {
		case Proposal, Deltas, Design, Tasks, State:
		default:
			return fmt.Errorf("gate_registry_invalid: gate %q covers unknown artifact %q; next: use a registered artifact", gate.ID, artifact)
		}
		if seen[artifact] {
			return fmt.Errorf("gate_registry_duplicate: gate %q repeats artifact %q; next: keep one artifact reference", gate.ID, artifact)
		}
		seen[artifact] = true
	}
	return nil
}
