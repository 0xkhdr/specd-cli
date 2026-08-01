package cmd

import (
	"fmt"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

type NextResult struct {
	Root           Root                   `json:"root"`
	Change         string                 `json:"change"`
	Revision       uint64                 `json:"revision"`
	Frontier       []string               `json:"frontier"`
	Selected       *core.TaskReadiness    `json:"selected,omitempty"`
	Classification string                 `json:"classification"`
	Blocker        *core.ReadinessBlocker `json:"blocker,omitempty"`
	Action         string                 `json:"action"`
}

// Next is a read-only view of the same frontier returned by Status. An empty
// task ID returns every eligible task; a non-empty ID only validates membership.
func Next(root, change, taskID string) (NextResult, error) {
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return NextResult{}, err
	}
	model := snapshot.Model()
	result := NextResult{
		Root: Root{Path: snapshot.Root()}, Change: snapshot.Change(), Revision: snapshot.StateRevision(),
		Frontier: model.Frontier(),
	}
	if taskID == "" {
		if len(result.Frontier) != 0 {
			result.Classification = "frontier"
			result.Action = "select any returned frontier task"
			return result, nil
		}
		outcome := model.Outcome()
		result.Classification, result.Blocker = outcome.Classification, outcome.Blocker
		switch outcome.Classification {
		case "all_complete":
			result.Action = "all task work is complete"
		case "waiting_approval":
			result.Action = outcome.Blocker.Action
			if handoff := snapshot.Handoff(); handoff != nil && handoff.HumanInstruction != "" {
				result.Action = handoff.HumanInstruction
			}
		default:
			result.Action = outcome.Blocker.Action
		}
		return result, nil
	}
	if model.InFrontier(taskID) {
		row, ok := model.Task(taskID)
		if !ok {
			return result, nextRefusal("plan_invalid", "author", taskID, "repair tasks and run check")
		}
		result.Selected = &row
		result.Classification = "selected"
		result.Action = fmt.Sprintf("request bounded context for %s", taskID)
		return result, nil
	}
	if row, ok := model.Task(taskID); ok {
		if row.Blocker == nil {
			return result, nextRefusal("plan_invalid", "author", taskID, "repair tasks and run check")
		}
		return result, nextRefusal(row.Blocker.Code, row.Blocker.Owner, taskID, row.Blocker.Action)
	}
	return result, nextRefusal("plan_invalid", "author", taskID, "repair tasks and run check")
}

func nextRefusal(code, owner, taskID, action string) error {
	return &NextRefusal{
		Refusal: failure.New(code, "", "", fmt.Sprintf("task %q is not in the current frontier", taskID), action),
		Owner:   owner,
		TaskID:  taskID,
	}
}

type NextRefusal struct {
	*failure.Refusal
	Owner  string
	TaskID string
}

func (refusal *NextRefusal) Unwrap() error { return refusal.Refusal }
