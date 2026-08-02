package cmd

import (
	"slices"

	"github.com/0xkhdr/specd-cli/internal/core"
)

type StartResult struct {
	Change         string   `json:"change"`
	TaskID         string   `json:"task"`
	AttemptID      string   `json:"attempt"`
	BaselineHEAD   string   `json:"baselineHead"`
	RevisionBefore uint64   `json:"revisionBefore"`
	RevisionAfter  uint64   `json:"revisionAfter"`
	DeclaredFiles  []string `json:"declaredFiles"`
	Assurance      string   `json:"assurance"`
}

// Start opens one attempt. An unauthorized or empty actor is refused by the
// core transition itself, which is the one owner of that boundary.
func Start(root, change, taskID string, expectedRevision uint64, actor string) (StartResult, error) {
	attempt, err := core.StartTaskAttempt(root, change, core.StartAttemptIntent{
		TaskID: taskID, ExpectedRevision: expectedRevision, Actor: actor,
	})
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		Change: attempt.Change, TaskID: attempt.TaskID, AttemptID: attempt.ID,
		BaselineHEAD:   attempt.BaselineHEAD,
		RevisionBefore: attempt.RevisionBefore, RevisionAfter: attempt.RevisionAfter,
		DeclaredFiles: slices.Clone(attempt.DeclaredFiles),
		Assurance:     attempt.Assurance,
	}, nil
}
