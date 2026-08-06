package cmd

import "github.com/0xkhdr/specd-cli/internal/core"

// Complete closes one task against applicable passing evidence. An empty or
// unauthorized actor is refused by the core transition, not restated here.
func Complete(root, change, taskID string, expectedRevision uint64, actor string) (core.Completion, error) {
	return core.CompleteTask(root, change, core.CompleteRequest{
		TaskID: taskID, ExpectedRevision: expectedRevision,
		Authority: core.AuthorizeCompletion(actor),
	})
}
