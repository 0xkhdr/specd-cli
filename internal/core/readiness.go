package core

import (
	"encoding/json"
	"fmt"

	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type Readiness string

const (
	ReadinessReady             Readiness = "ready"
	ReadinessWaitingDependency Readiness = "waiting_dependency"
	ReadinessWaitingApproval   Readiness = "waiting_approval"
	ReadinessActive            Readiness = "active"
	ReadinessTerminal          Readiness = "terminal"
	ReadinessBlocked           Readiness = "blocked"
)

type ReadinessBlocker struct {
	Code   string `json:"code"`
	Owner  string `json:"owner"`
	Action string `json:"action"`
}

type TaskReadiness struct {
	ID           string            `json:"id"`
	Activity     TaskActivity      `json:"activity"`
	Readiness    Readiness         `json:"readiness"`
	Wave         int               `json:"wave"`
	Dependencies []string          `json:"dependencies"`
	Blocker      *ReadinessBlocker `json:"blocker,omitempty"`
}

type ReadinessModel struct {
	tasks       []TaskReadiness
	frontier    []string
	planInvalid *ReadinessBlocker
	primary     *ReadinessBlocker
	global      *ReadinessBlocker
}

type ReadinessOutcome struct {
	Classification string            `json:"classification"`
	Blocker        *ReadinessBlocker `json:"blocker,omitempty"`
}

type ReadinessSnapshot struct {
	root       string
	change     string
	current    state.State
	projection state.Projection
	approval   ApprovalStatus
	handoff    *ApprovalHandoff
	model      ReadinessModel
	authored   plan.Change
}

func (snapshot ReadinessSnapshot) Root() string                 { return snapshot.root }
func (snapshot ReadinessSnapshot) Change() string               { return snapshot.change }
func (snapshot ReadinessSnapshot) StateRevision() uint64        { return snapshot.current.Revision }
func (snapshot ReadinessSnapshot) Projection() state.Projection { return snapshot.projection }
func (snapshot ReadinessSnapshot) Model() ReadinessModel        { return snapshot.model }
func (snapshot ReadinessSnapshot) Plan() plan.Change            { return clonePlanChange(snapshot.authored) }
func (snapshot ReadinessSnapshot) Approval() ApprovalStatus {
	return cloneApprovalStatus(snapshot.approval)
}
func (snapshot ReadinessSnapshot) Handoff() *ApprovalHandoff {
	if snapshot.handoff == nil {
		return nil
	}
	copy := *snapshot.handoff
	copy.Findings = append([]gates.Finding(nil), snapshot.handoff.Findings...)
	copy.ReviewedArtifacts = append([]ApprovalArtifact(nil), snapshot.handoff.ReviewedArtifacts...)
	return &copy
}
func (snapshot ReadinessSnapshot) Valid() bool {
	return snapshot.root != "" && snapshot.change != "" && snapshot.current.Revision != 0
}

func (model ReadinessModel) Tasks() []TaskReadiness {
	rows := make([]TaskReadiness, len(model.tasks))
	for index, row := range model.tasks {
		rows[index] = cloneTaskReadiness(row)
	}
	return rows
}

func (model ReadinessModel) Frontier() []string {
	return append([]string(nil), model.frontier...)
}

func (model ReadinessModel) InFrontier(id string) bool {
	for _, candidate := range model.frontier {
		if candidate == id {
			return true
		}
	}
	return false
}

func (model ReadinessModel) Task(id string) (TaskReadiness, bool) {
	for _, row := range model.tasks {
		if row.ID == id {
			return cloneTaskReadiness(row), true
		}
	}
	return TaskReadiness{}, false
}

func (model ReadinessModel) PlanBlocker() *ReadinessBlocker {
	if model.planInvalid == nil {
		return nil
	}
	blocker := *model.planInvalid
	return &blocker
}

func (model ReadinessModel) PrimaryBlocker() *ReadinessBlocker {
	return copyBlocker(model.primary)
}

// CanRetry reports whether a failed task may return to in_progress. It is
// frontier membership with one substitution — the task is failed instead of
// pending — so the rule stays in this owner with the frontier rule.
func (model ReadinessModel) CanRetry(id string) bool {
	if model.global != nil || model.planInvalid != nil {
		return false
	}
	row, found := model.Task(id)
	if !found || row.Activity != TaskFailed {
		return false
	}
	for _, dependency := range row.Dependencies {
		predecessor, found := model.Task(dependency)
		if !found || predecessor.Activity != TaskCompleted {
			return false
		}
	}
	return true
}

func (model ReadinessModel) AllComplete() bool {
	if model.planInvalid != nil || model.global != nil {
		return false
	}
	for _, row := range model.tasks {
		if row.Activity != TaskCompleted {
			return false
		}
	}
	return true
}

func (model ReadinessModel) Outcome() ReadinessOutcome {
	if len(model.frontier) != 0 {
		return ReadinessOutcome{Classification: "frontier"}
	}
	if model.AllComplete() {
		return ReadinessOutcome{Classification: "all_complete"}
	}
	blocker := model.PrimaryBlocker()
	if blocker == nil {
		blocker = blockerForInvalidPlan()
	}
	classification := "task_blocked"
	switch blocker.Code {
	case "plan_invalid":
		classification = "plan_invalid"
	case "approval_stale", "change_not_approved":
		classification = "waiting_approval"
	case "dependency_incomplete":
		classification = "waiting_dependency"
	}
	return ReadinessOutcome{Classification: classification, Blocker: blocker}
}

// LoadReadinessSnapshot reads every readiness/status input under one change
// lock. Consumers render this value and perform no recovery or mutation.
func LoadReadinessSnapshot(root, change string) (ReadinessSnapshot, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return ReadinessSnapshot{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ReadinessSnapshot{}, err
	}
	var result ReadinessSnapshot
	err = lock.With(lockPath, func() error {
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		raw, err := readCheckState(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(raw, change)
		if err != nil {
			return err
		}
		registry, err := gates.PlanningRegistry()
		if err != nil {
			return err
		}
		authored := plan.ParseChange(owner, change)
		approval, err := projectApprovalStatusForPlanLocked(
			owner, change, registry.Version(), DefaultPolicyDigest(), authored,
		)
		if err != nil {
			return err
		}
		checkSnapshot := CheckSnapshot{
			root: owner.Root(), change: change, registry: registry,
			snapshot: gates.Snapshot{
				Plan: authored, Lifecycle: current.Stage, StateRevision: current.Revision,
				Transition: gates.PlanningToApproved, RegistryVersion: registry.Version(),
				PolicyDigest: DefaultPolicyDigest(),
			},
		}
		var handoff *ApprovalHandoff
		if current.Stage == "planning" {
			handoff, err = approvalHandoffLocked(owner, checkSnapshot)
		} else {
			handoff, err = approvalHandoffForStatus(change, approval, nil)
		}
		if err != nil {
			return err
		}
		result = ReadinessSnapshot{
			root: owner.Root(), change: change, current: current,
			projection: current.Project(), approval: approval, handoff: handoff,
			model:    ProjectReadiness(authored.Tasks, current.Tasks, current.Stage, approval),
			authored: authored,
		}
		return nil
	})
	if err != nil {
		return ReadinessSnapshot{}, fmt.Errorf("load readiness snapshot: %w", err)
	}
	return result, nil
}

// ProjectReadiness is pure: its canonical plan, persisted activity values,
// lifecycle, and approval projection are value inputs; it performs no I/O.
func ProjectReadiness(tasks plan.Tasks, persisted map[string]json.RawMessage, lifecycle string, approval ApprovalStatus) ReadinessModel {
	graph, err := BuildTaskGraph(tasks)
	if err != nil {
		return invalidReadinessModel()
	}
	authored := graph.AuthoredOrder()
	activities, err := ProjectTaskActivities(authored, persisted)
	if err != nil {
		return invalidReadinessModel()
	}
	global := readinessApprovalBlocker(lifecycle, approval)
	if global != nil && global.Code == "plan_invalid" {
		return invalidReadinessModel()
	}
	byID := make(map[string]TaskActivity, len(activities))
	for _, row := range activities {
		byID[row.ID] = row.Activity
	}
	model := ReadinessModel{
		tasks:    make([]TaskReadiness, 0, len(authored)),
		frontier: []string{},
		primary:  copyBlocker(global),
		global:   copyBlocker(global),
	}
	for _, id := range authored {
		dependencies := graph.Dependencies(id)
		wave, ok := graph.Wave(id)
		if !ok {
			return invalidReadinessModel()
		}
		row := TaskReadiness{
			ID: id, Activity: byID[id], Wave: wave,
			Dependencies: dependencies,
		}
		row.Readiness, row.Blocker = deriveTaskReadiness(row.Activity, dependencies, byID, global)
		if row.Readiness == ReadinessReady {
			model.frontier = append(model.frontier, id)
		} else if blockerRank(row.Blocker) < blockerRank(model.primary) {
			model.primary = copyBlocker(row.Blocker)
		}
		model.tasks = append(model.tasks, row)
	}
	return model
}

func blockerRank(value *ReadinessBlocker) int {
	if value == nil {
		return 99
	}
	ranks := map[string]int{
		"plan_invalid": 0, "lifecycle_unsupported": 1,
		"approval_stale": 2, "change_not_approved": 3,
		"task_blocked": 4, "task_active": 5, "task_completed": 6,
		"dependency_incomplete": 7,
	}
	if rank, ok := ranks[value.Code]; ok {
		return rank
	}
	return 0
}

func deriveTaskReadiness(activity TaskActivity, dependencies []string, activities map[string]TaskActivity, global *ReadinessBlocker) (Readiness, *ReadinessBlocker) {
	if global != nil {
		return ReadinessWaitingApproval, copyBlocker(global)
	}
	switch activity {
	case TaskBlocked, TaskFailed:
		return ReadinessBlocked, blocker("task_blocked", "human", "resolve the task blocker and return the task to pending")
	case TaskInProgress:
		return ReadinessActive, blocker("task_active", "agent", "continue the active task")
	case TaskCompleted:
		return ReadinessTerminal, blocker("task_completed", "harness", "continue with remaining frontier tasks")
	case TaskPending:
		for _, dependency := range dependencies {
			if activities[dependency] != TaskCompleted {
				return ReadinessWaitingDependency, blocker("dependency_incomplete", "agent", "complete the listed dependency tasks")
			}
		}
		return ReadinessReady, nil
	default:
		return ReadinessBlocked, blocker("plan_invalid", "author", "repair tasks and run check")
	}
}

func readinessApprovalBlocker(lifecycle string, approval ApprovalStatus) *ReadinessBlocker {
	switch lifecycle {
	case "approved":
		if approval.Current {
			return nil
		}
		if approval.Approval != nil {
			return blocker("approval_stale", "human", "rerun check, then obtain fresh human approval")
		}
		return blocker("change_not_approved", "human", "run check, then obtain human approval")
	case "planning":
		if approval.Approval != nil {
			return blocker("approval_stale", "human", "rerun check, then obtain fresh human approval")
		}
		return blocker("change_not_approved", "human", "run check, then obtain human approval")
	case string(LifecycleExecuting), string(LifecycleReconciling), string(LifecycleArchived):
		// Legal states that no stage writes yet: approve is the only stage
		// writer today. Readiness projection for them arrives with the stage
		// that starts writing them. Until then the refusal must not tell an
		// author to repair state that is perfectly legal.
		return blocker("lifecycle_unsupported", "harness",
			"select a planning or approved change")
	default:
		// Unknown lifecycle is corrupt input and stays fail-closed.
		return blocker("plan_invalid", "author", "repair lifecycle state and run check")
	}
}

func invalidReadinessModel() ReadinessModel {
	invalid := blockerForInvalidPlan()
	return ReadinessModel{
		tasks: []TaskReadiness{}, frontier: []string{},
		planInvalid: copyBlocker(invalid),
		primary:     copyBlocker(invalid),
		global:      copyBlocker(invalid),
	}
}

func blockerForInvalidPlan() *ReadinessBlocker {
	return blocker("plan_invalid", "author", "repair tasks and run check")
}

func blocker(code, owner, action string) *ReadinessBlocker {
	return &ReadinessBlocker{Code: code, Owner: owner, Action: action}
}

func copyBlocker(value *ReadinessBlocker) *ReadinessBlocker {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneTaskReadiness(row TaskReadiness) TaskReadiness {
	row.Dependencies = append([]string(nil), row.Dependencies...)
	row.Blocker = copyBlocker(row.Blocker)
	return row
}

func cloneApprovalStatus(value ApprovalStatus) ApprovalStatus {
	if value.Approval != nil {
		approval := *value.Approval
		approval.Artifacts = append([]ApprovalArtifact(nil), value.Approval.Artifacts...)
		value.Approval = &approval
	}
	return value
}

func clonePlanChange(value plan.Change) plan.Change {
	value.Proposal.Source = clonePlanSource(value.Proposal.Source)
	value.Proposal.Sections = clonePlanSections(value.Proposal.Sections)
	value.Proposal.Diagnostics = append([]plan.Diagnostic(nil), value.Proposal.Diagnostics...)
	value.Design.Source = clonePlanSource(value.Design.Source)
	value.Design.Sections = clonePlanSections(value.Design.Sections)
	value.Design.References = append([]plan.RequirementReference(nil), value.Design.References...)
	value.Design.Diagnostics = append([]plan.Diagnostic(nil), value.Design.Diagnostics...)
	value.Tasks.Source = clonePlanSource(value.Tasks.Source)
	value.Tasks.Tasks = append([]plan.Task(nil), value.Tasks.Tasks...)
	for index := range value.Tasks.Tasks {
		task := &value.Tasks.Tasks[index]
		task.Files = append([]string(nil), task.Files...)
		task.DependsOn = append([]string(nil), task.DependsOn...)
		task.References = append([]plan.RequirementReference(nil), task.References...)
		task.Source = append([]byte(nil), task.Source...)
	}
	value.Tasks.Waves = append([]plan.TaskWave(nil), value.Tasks.Waves...)
	for index := range value.Tasks.Waves {
		value.Tasks.Waves[index].Tasks = append([]string(nil), value.Tasks.Waves[index].Tasks...)
	}
	value.Tasks.Diagnostics = append([]plan.Diagnostic(nil), value.Tasks.Diagnostics...)
	value.Deltas = append([]plan.CapabilityDelta(nil), value.Deltas...)
	for index := range value.Deltas {
		delta := &value.Deltas[index]
		delta.Source = clonePlanSource(delta.Source)
		delta.Purpose = append([]byte(nil), delta.Purpose...)
		delta.Diagnostics = append([]plan.Diagnostic(nil), delta.Diagnostics...)
		delta.Operations = append([]plan.Operation(nil), delta.Operations...)
		for operationIndex := range delta.Operations {
			operation := &delta.Operations[operationIndex]
			operation.Raw = append([]byte(nil), operation.Raw...)
			if operation.Requirement != nil {
				requirement := *operation.Requirement
				requirement.Raw = append([]byte(nil), requirement.Raw...)
				requirement.Scenarios = append([]plan.Scenario(nil), requirement.Scenarios...)
				for scenarioIndex := range requirement.Scenarios {
					requirement.Scenarios[scenarioIndex].Raw =
						append([]byte(nil), requirement.Scenarios[scenarioIndex].Raw...)
				}
				operation.Requirement = &requirement
			}
		}
	}
	value.Trace.Requirements = append([]plan.RequirementTrace(nil), value.Trace.Requirements...)
	for index := range value.Trace.Requirements {
		value.Trace.Requirements[index].Tasks = append([]string(nil), value.Trace.Requirements[index].Tasks...)
	}
	value.Trace.Tasks = append([]plan.TaskTrace(nil), value.Trace.Tasks...)
	for index := range value.Trace.Tasks {
		value.Trace.Tasks[index].Requirements =
			append([]plan.RequirementReference(nil), value.Trace.Tasks[index].Requirements...)
	}
	value.Diagnostics = append([]plan.Diagnostic(nil), value.Diagnostics...)
	return value
}

func clonePlanSource(source plan.Source) plan.Source {
	source.Bytes = append([]byte(nil), source.Bytes...)
	return source
}

func clonePlanSections(sections []plan.Section) []plan.Section {
	result := append([]plan.Section(nil), sections...)
	for index := range result {
		result[index].Content = append([]byte(nil), result[index].Content...)
	}
	return result
}
