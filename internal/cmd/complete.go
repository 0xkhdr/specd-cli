package cmd

import (
	"errors"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
)

type CompleteOptions struct {
	Actor string
}

func Complete(root, change, taskID string, expectedRevision uint64, options CompleteOptions) (core.Completion, error) {
	if strings.TrimSpace(options.Actor) == "" {
		return core.Completion{}, errors.New(
			"complete actor is required; next: retry through an authorized harness operation",
		)
	}
	return core.CompleteTask(root, change, core.CompleteRequest{
		TaskID: taskID, ExpectedRevision: expectedRevision,
		Authority: core.AuthorizeCompletion(options.Actor),
	})
}
