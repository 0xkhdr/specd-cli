package plan

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

type RequirementTrace struct {
	Capability  string
	Requirement string
	Location    Location
	Tasks       []string
}

type TaskTrace struct {
	TaskID       string
	Requirements []RequirementReference
}

type Trace struct {
	Requirements []RequirementTrace
	Tasks        []TaskTrace
}

// Change is the only semantic projection of one authored change.
type Change struct {
	Root        string
	Name        string
	Proposal    Proposal
	Deltas      []CapabilityDelta
	Design      Design
	Tasks       Tasks
	Trace       Trace
	Diagnostics []Diagnostic
}

// Inspection and Validation deliberately point at the same canonical result.
// Later consumers may add view-only helpers without reading Markdown again.
type Inspection struct{ Canonical *Change }
type Validation struct{ Canonical *Change }

func (change *Change) Inspection() Inspection { return Inspection{Canonical: change} }
func (change *Change) Validation() Validation { return Validation{Canonical: change} }

func ParseChange(owner *corepath.Owner, name string) Change {
	result := Change{Root: owner.Root(), Name: name}
	if err := corepath.ValidateSegment(name); err != nil {
		result.Diagnostics = []Diagnostic{diagnostic(
			"change_unsafe", Location{Path: name, Line: 1, Column: 1},
			err.Error(), "choose a lowercase kebab-case change name",
		)}
		return result
	}
	changePath, err := owner.Change(name)
	if err != nil {
		changePath = filepath.Join(owner.Changes(), name)
		result.Diagnostics = []Diagnostic{pathDiagnostic("change_path", changePath, err)}
		return result
	}
	changeInfo, err := os.Lstat(changePath)
	if err != nil || !changeInfo.IsDir() || changeInfo.Mode()&os.ModeSymlink != 0 {
		result.Diagnostics = []Diagnostic{pathDiagnostic("change_path", changePath, err)}
		return result
	}
	proposalPath, proposalPathErr := owner.ChangeProposal(name)
	designPath, designPathErr := owner.ChangeDesign(name)
	tasksPath, tasksPathErr := owner.ChangeTasks(name)
	proposalPath = pathOr(proposalPath, filepath.Join(changePath, "proposal.md"))
	designPath = pathOr(designPath, filepath.Join(changePath, "design.md"))
	tasksPath = pathOr(tasksPath, filepath.Join(changePath, "tasks.md"))

	proposalSource, proposalRead := readPlanSource(proposalPath, proposalPathErr)
	designSource, designRead := readPlanSource(designPath, designPathErr)
	tasksSource, tasksRead := readPlanSource(tasksPath, tasksPathErr)
	result.Proposal = Proposal{Source: proposalSource}
	if len(proposalRead) == 0 {
		result.Proposal = ParseProposal(proposalSource)
	}
	result.Design = Design{Source: designSource}
	if len(designRead) == 0 {
		result.Design = ParseDesign(designSource)
	}
	result.Tasks = Tasks{Source: tasksSource}
	if len(tasksRead) == 0 {
		result.Tasks = ParseTasks(tasksSource)
	}

	deltas, discoveryDiagnostics := DiscoverCapabilityDeltas(owner, name)
	result.Deltas = deltas
	for i := range discoveryDiagnostics {
		if discoveryDiagnostics[i].Location.Path == "" {
			discoveryDiagnostics[i].Location.Path = filepath.Join(changePath, "specs")
		}
	}
	if len(deltas) == 0 && len(discoveryDiagnostics) == 0 {
		specsPath, err := owner.ChangeSpecs(name)
		if err != nil {
			discoveryDiagnostics = append(discoveryDiagnostics, pathDiagnostic("delta_path", specsPath, err))
		} else {
			discoveryDiagnostics = append(discoveryDiagnostics, diagnostic(
				"delta_missing", Location{Path: specsPath, Line: 1, Column: 1},
				"change has no capability delta", "create specs/<capability>/spec.md",
			))
		}
	}

	result.Diagnostics = append(result.Diagnostics, proposalRead...)
	result.Diagnostics = append(result.Diagnostics, designRead...)
	result.Diagnostics = append(result.Diagnostics, tasksRead...)
	result.Diagnostics = append(result.Diagnostics, result.Proposal.Diagnostics...)
	result.Diagnostics = append(result.Diagnostics, result.Design.Diagnostics...)
	result.Diagnostics = append(result.Diagnostics, result.Tasks.Diagnostics...)
	result.Diagnostics = append(result.Diagnostics, discoveryDiagnostics...)
	for _, delta := range result.Deltas {
		result.Diagnostics = append(result.Diagnostics, delta.Diagnostics...)
	}

	trace, traceDiagnostics := projectTrace(result.Deltas, result.Design, result.Tasks)
	result.Trace = trace
	result.Diagnostics = append(result.Diagnostics, traceDiagnostics...)
	sortDiagnostics(result.Diagnostics)
	return result
}

func readPlanSource(path string, pathErr error) (Source, []Diagnostic) {
	source := Source{Path: path}
	if pathErr != nil {
		return source, []Diagnostic{pathDiagnostic("artifact_path", path, pathErr)}
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return source, nil
	}
	if err != nil {
		return source, []Diagnostic{pathDiagnostic("artifact_unreadable", path, err)}
	}
	defer file.Close()
	opened, statErr := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() ||
		!pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(opened, pathInfo) {
		return source, []Diagnostic{diagnostic(
			"artifact_unsafe", Location{Path: path, Line: 1, Column: 1},
			"planning artifact must be a regular non-symlink file",
			"replace it with a regular file inside the change",
		)}
	}
	source.Present = true
	raw, err := io.ReadAll(file)
	if err != nil {
		return source, []Diagnostic{pathDiagnostic("artifact_unreadable", path, err)}
	}
	after, afterErr := file.Stat()
	namedAfter, namedErr := os.Lstat(path)
	if afterErr != nil || namedErr != nil || !os.SameFile(opened, after) ||
		!os.SameFile(after, namedAfter) || opened.Size() != after.Size() ||
		!opened.ModTime().Equal(after.ModTime()) || opened.Mode() != after.Mode() {
		return source, []Diagnostic{diagnostic(
			"artifact_unsafe", Location{Path: path, Line: 1, Column: 1},
			"planning artifact changed during read", "retry after edits stop",
		)}
	}
	// A second read detects same-size changes even on coarse timestamp filesystems.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return source, []Diagnostic{pathDiagnostic("artifact_unreadable", path, err)}
	}
	again, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(raw, again) {
		return source, []Diagnostic{diagnostic(
			"artifact_unsafe", Location{Path: path, Line: 1, Column: 1},
			"planning artifact changed during read", "retry after edits stop",
		)}
	}
	source.Bytes = raw
	return source, nil
}

type traceTarget struct {
	capability, requirement string
	location                Location
	tasks                   []string
	taskSeen                map[string]bool
}

func projectTrace(deltas []CapabilityDelta, design Design, tasks Tasks) (Trace, []Diagnostic) {
	targets := map[string][]*traceTarget{}
	var orderedTargets []*traceTarget
	for _, delta := range deltas {
		for _, operation := range delta.Operations {
			names := operationRequirementNames(operation)
			for _, name := range names {
				target := &traceTarget{
					capability: delta.Capability, requirement: name,
					location: operation.Location, taskSeen: map[string]bool{},
				}
				key := traceKey(delta.Capability, name)
				targets[key] = append(targets[key], target)
				orderedTargets = append(orderedTargets, target)
			}
		}
	}

	var diagnostics []Diagnostic
	resolve := func(reference RequirementReference, owner string, taskID string) {
		matches := targets[traceKey(reference.Capability, reference.Requirement)]
		switch len(matches) {
		case 0:
			diagnostics = append(diagnostics, diagnostic(
				"trace_unknown", reference.Location,
				owner+" cites unknown requirement "+reference.Capability+"/Requirement: "+reference.Requirement,
				"cite one requirement declared by this change",
			))
		case 1:
			if taskID != "" && !matches[0].taskSeen[taskID] {
				matches[0].tasks = append(matches[0].tasks, taskID)
				matches[0].taskSeen[taskID] = true
			}
		default:
			diagnostics = append(diagnostics, diagnostic(
				"trace_ambiguous", reference.Location,
				owner+" cites an ambiguous requirement identity",
				"remove duplicate or contradictory requirement operations",
			))
		}
	}

	// Design references resolve on their own; unrelated section diagnostics must
	// not hide an unknown citation. An unreadable or unsafe design has no parsed
	// references at all, so it still cannot cascade.
	for _, reference := range design.References {
		// A scaffold placeholder name is already diagnosed as a placeholder;
		// resolving it would only restate that in trace terms.
		if placeholderPattern.MatchString(reference.Requirement) {
			continue
		}
		resolve(reference, "design", "")
	}
	trace := Trace{Tasks: make([]TaskTrace, 0, len(tasks.Tasks))}
	for _, task := range tasks.Tasks {
		if !task.Valid {
			continue
		}
		taskTrace := TaskTrace{TaskID: task.ID, Requirements: append([]RequirementReference(nil), task.References...)}
		trace.Tasks = append(trace.Tasks, taskTrace)
		for _, reference := range task.References {
			resolve(reference, "task "+task.ID, task.ID)
		}
	}

	for _, target := range orderedTargets {
		trace.Requirements = append(trace.Requirements, RequirementTrace{
			Capability: target.capability, Requirement: target.requirement,
			Location: target.location, Tasks: append([]string(nil), target.tasks...),
		})
		if len(target.tasks) == 0 {
			diagnostics = append(diagnostics, diagnostic(
				"trace_uncovered", target.location,
				target.capability+"/Requirement: "+target.requirement+" has no implementing task",
				"add the requirement reference to one task",
			))
		}
	}
	sortDiagnostics(diagnostics)
	return trace, diagnostics
}

func operationRequirementNames(operation Operation) []string {
	if operation.Requirement != nil {
		return []string{operation.Requirement.Name}
	}
	if operation.Kind == Renamed {
		return []string{operation.To}
	}
	return nil
}

func traceKey(capability, requirement string) string {
	return capability + "\x00" + NormalizeRequirementIdentity(requirement)
}

func pathOr(path, fallback string) string {
	if path != "" {
		return path
	}
	return fallback
}
