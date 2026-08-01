package cmd

import (
	"errors"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
)

type StartOptions struct {
	Actor string
}

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

func Start(root, change, taskID string, expectedRevision uint64, options StartOptions) (StartResult, error) {
	if strings.TrimSpace(options.Actor) == "" {
		return StartResult{}, errors.New(
			"start actor is required; next: retry through an authorized harness operation",
		)
	}
	attempt, err := core.StartTaskAttempt(root, change, core.StartAttemptIntent{
		TaskID: taskID, ExpectedRevision: expectedRevision, Actor: options.Actor,
	})
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		Change: attempt.Change, TaskID: attempt.TaskID, AttemptID: attempt.ID,
		BaselineHEAD:   attempt.BaselineHEAD,
		RevisionBefore: attempt.RevisionBefore, RevisionAfter: attempt.RevisionAfter,
		DeclaredFiles: append([]string(nil), attempt.DeclaredFiles...),
		Assurance:     attempt.Assurance,
	}, nil
}
