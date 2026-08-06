package cmd

import (
	"fmt"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/report"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

type Root struct {
	Path string `json:"path"`
}

type ActivityCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Blocked    int `json:"blocked"`
}

type StatusNextAction struct {
	Kind      string `json:"kind"`
	Operation string `json:"operation,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Action    string `json:"action"`
}

// StatusResult is canonical status. Policy, assurance, review approvability,
// and deferred-domain eligibility are not restated here: `report` owns those
// projections, and a second copy of them is a second source of truth.
type StatusResult struct {
	Root             Root                  `json:"root"`
	Approval         *core.ApprovalHandoff `json:"approval,omitempty"`
	ApprovalStatus   core.ApprovalStatus   `json:"approvalStatus"`
	Counts           ActivityCounts        `json:"counts"`
	Tasks            []core.TaskReadiness  `json:"tasks"`
	Frontier         []string              `json:"frontier"`
	Next             StatusNextAction      `json:"next"`
	AllTasksComplete bool                  `json:"allTasksComplete"`
	state.Projection
}

// Status projects canonical status under the default policy. It loads truth
// once through the report owner, so status and every report read the same
// snapshot rather than two parsers of the same files.
func Status(root, change string) (StatusResult, error) {
	truth, err := report.Load(root, change, core.DefaultPolicy())
	if err != nil {
		return StatusResult{}, err
	}
	snapshot := truth.Snapshot
	result := StatusResult{
		Root: Root{Path: snapshot.Root()}, Approval: snapshot.Handoff(),
		ApprovalStatus: snapshot.Approval(), Projection: snapshot.Projection(),
	}
	readiness := snapshot.Model()
	result.Tasks = readiness.Tasks()
	result.Frontier = readiness.Frontier()
	result.AllTasksComplete = readiness.AllComplete()
	result.Counts = countActivities(result.Tasks)
	result.Next = statusNextAction(change, readiness, result.Approval)
	return result, nil
}

func countActivities(tasks []core.TaskReadiness) ActivityCounts {
	var counts ActivityCounts
	for _, task := range tasks {
		switch task.Activity {
		case core.TaskPending:
			counts.Pending++
		case core.TaskInProgress:
			counts.InProgress++
		case core.TaskCompleted:
			counts.Completed++
		case core.TaskFailed:
			counts.Failed++
		case core.TaskBlocked:
			counts.Blocked++
		}
	}
	return counts
}

func statusNextAction(change string, readiness core.ReadinessModel, handoff *core.ApprovalHandoff) StatusNextAction {
	outcome := readiness.Outcome()
	if outcome.Classification == "frontier" {
		return StatusNextAction{
			Kind: "operation", Operation: "next",
			Action: fmt.Sprintf("run specd next %s", change),
		}
	}
	if outcome.Classification == "all_complete" {
		return StatusNextAction{Kind: "terminal", Action: "all task work is complete"}
	}
	blocker := outcome.Blocker
	if blocker == nil {
		return StatusNextAction{Kind: "blocked", Owner: "author", Action: "repair tasks and run check"}
	}
	if blocker.Code == "approval_stale" || blocker.Code == "change_not_approved" {
		action := blocker.Action
		if handoff != nil && handoff.HumanInstruction != "" {
			action = handoff.HumanInstruction
		}
		return StatusNextAction{Kind: "human_handoff", Owner: "human", Action: action}
	}
	return StatusNextAction{Kind: "blocked", Owner: blocker.Owner, Action: blocker.Action}
}
