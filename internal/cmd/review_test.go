package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
)

const (
	reviewGitEmail = "human@example.com"
	reviewTrusted  = "reviewer@example.com"
)

func TestProductionReviewCommandRecordsOneCurrentVerdict(t *testing.T) {
	const (
		change = "sample-loop"
		taskID = "edit-sample"
		agent  = "agent:builder"
	)
	root, attempt := productionAttempt(t, change, taskID, agent)
	options := ReviewOptions{Actor: agent}

	// Projection first: it writes nothing and reports the bounded packet.
	projected, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		options, reviewGitEmail, reviewTrusted)
	if err != nil || projected.Verdict.State != evidence.ReviewMissing ||
		projected.PacketHash == "" || projected.RecordID != "" {
		t.Fatalf("projection = %#v, %v", projected, err)
	}
	if projected.Approvable || len(projected.Blockers) == 0 {
		t.Fatal("an unproven change presented an approvable packet")
	}

	recorded, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		ReviewOptions{Actor: agent, Verdict: VerdictApprove, Reviewer: reviewTrusted},
		reviewGitEmail, reviewTrusted)
	if err != nil || recorded.Verdict.State != evidence.ReviewApproved ||
		recorded.Verdict.Reviewer != reviewTrusted || recorded.RecordID == "" {
		t.Fatalf("verdict = %#v, %v", recorded, err)
	}
	current, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		options, reviewGitEmail, reviewTrusted)
	// Recording the verdict moves neither binding it was taken against, so the
	// verdict it just wrote reads back as current.
	if err != nil || current.Verdict.State != evidence.ReviewApproved ||
		current.PacketHash != recorded.PacketHash ||
		current.EvidenceSet != recorded.EvidenceSet {
		t.Fatalf("current = %#v, %v", current, err)
	}

	// Evidence drift: a later observation changes the set the reviewer read.
	if _, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: agent, Profile: "production", Class: evidence.ClassBuild,
		CheckID: "compile", Command: "go build ./...",
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		options, reviewGitEmail, reviewTrusted)
	if err != nil || stale.Verdict.State != evidence.ReviewStale {
		t.Fatalf("evidence drift = %#v, %v", stale, err)
	}

	// Code drift: the review inputs are no longer current, so review fails
	// closed instead of reporting a verdict against a moved commit.
	loopGit(t, root, []string{"commit", "--allow-empty", "-m", "drift", "--no-gpg-sign"})
	if drifted, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		options, reviewGitEmail, reviewTrusted); err == nil {
		t.Fatalf("code drift accepted: %#v", drifted)
	}
}

func TestProductionReviewCommandFailsClosedOnActorAndVerdict(t *testing.T) {
	const (
		change = "sample-loop"
		taskID = "edit-sample"
		agent  = "agent:builder"
	)
	root, attempt := productionAttempt(t, change, taskID, agent)
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	before, _ := os.ReadFile(evidencePath)

	for name, testCase := range map[string]struct {
		options              ReviewOptions
		gitEmail, reviewerID string
	}{
		"unknown verdict": {ReviewOptions{Actor: agent, Verdict: "maybe"}, reviewGitEmail, reviewTrusted},
		"unknown reviewer": {
			ReviewOptions{Actor: agent, Verdict: VerdictApprove}, "", "",
		},
		"reviewer is the approver": {
			ReviewOptions{Actor: agent, Verdict: VerdictApprove}, reviewGitEmail, reviewGitEmail,
		},
		"reviewer is the implementer": {
			ReviewOptions{Actor: agent, Verdict: VerdictApprove}, reviewGitEmail, agent,
		},
		"claim disagrees with trusted identity": {
			ReviewOptions{Actor: agent, Verdict: VerdictApprove, Reviewer: "someone@example.com"},
			reviewGitEmail, reviewTrusted,
		},
		"reject without findings": {
			ReviewOptions{Actor: agent, Verdict: VerdictReject}, reviewGitEmail, reviewTrusted,
		},
		"missing recording actor": {
			ReviewOptions{Verdict: VerdictApprove}, reviewGitEmail, reviewTrusted,
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
				testCase.options, testCase.gitEmail, testCase.reviewerID)
			if err == nil {
				t.Fatalf("accepted: %#v", result)
			}
			after, _ := os.ReadFile(evidencePath)
			if string(after) != string(before) {
				t.Fatal("a refused review wrote evidence")
			}
		})
	}

	// A reject with findings is recorded, blocks, and never proves anything.
	rejected, err := reviewWithSources(root, change, taskID, attempt.AttemptID,
		ReviewOptions{
			Actor: agent, Verdict: VerdictReject,
			Findings: "declared scope is wider than the task needs",
		}, reviewGitEmail, reviewTrusted)
	if err != nil || rejected.Verdict.State != evidence.ReviewRejected ||
		rejected.Findings == "" || rejected.RecordID == "" {
		t.Fatalf("reject = %#v, %v", rejected, err)
	}
}
