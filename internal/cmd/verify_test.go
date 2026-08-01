package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

func TestVerifyCommandRequiresExplicitShellAndPreservesArgv(t *testing.T) {
	request, err := verifyexec.ParseCommand(`go test ./internal/cmd -run 'TestVerify|TestEvidence'`)
	if err != nil || request.Shell != "" ||
		!reflect.DeepEqual(request.Argv, []string{"go", "test", "./internal/cmd", "-run", "TestVerify|TestEvidence"}) {
		t.Fatalf("argv = %#v, %v", request, err)
	}
	request, err = verifyexec.ParseCommand(`shell: printf ok | grep ok`)
	if err != nil || request.Shell != "printf ok | grep ok" || len(request.Argv) != 0 {
		t.Fatalf("shell = %#v, %v", request, err)
	}
	for _, malformed := range []string{"", "go test | tee out", `"unterminated`, `shell: `} {
		if _, err := verifyexec.ParseCommand(malformed); err == nil {
			t.Fatalf("malformed command accepted: %q", malformed)
		}
	}
}

func TestVerifyRefusalNeverCompletesOrWritesEvidence(t *testing.T) {
	root := statusReadinessRoot(t)
	statePath := mustStatePath(t, root, "safe-change")
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeEvidence, _ := os.ReadFile(evidencePath)
	result, err := Verify(context.Background(), root, "safe-change", "active", "missing", VerifyOptions{
		Actor: "agent:builder",
	})
	afterState, _ := os.ReadFile(statePath)
	afterEvidence, _ := os.ReadFile(evidencePath)
	if err == nil || result.Complete || string(beforeState) != string(afterState) ||
		string(beforeEvidence) != string(afterEvidence) {
		t.Fatalf("verify refusal = %#v, %v", result, err)
	}
}

func TestProductionEvidenceRunsThroughTheOneRunner(t *testing.T) {
	const (
		change = "sample-loop"
		taskID = "edit-sample"
		agent  = "agent:builder"
	)
	root, attempt := productionAttempt(t, change, taskID, agent)

	// Build proof: the stage-5 runner, a distinct class, and a check id the
	// current policy declares.
	built, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: agent, Profile: "production", Class: evidence.ClassBuild,
		CheckID: "compile", Command: "go build ./...",
	})
	if err != nil || built.Production == nil || built.RecordID == "" {
		t.Fatalf("build verify = %#v, %v", built, err)
	}
	if !built.Production.Passed || built.Production.Class != evidence.ClassBuild ||
		built.Production.CheckID != "compile" || built.Production.CommandHash == "" ||
		built.Production.PolicyDigest != core.ProductionPolicy().Digest() || built.Complete {
		t.Fatalf("build evidence = %#v", built.Production)
	}
	// It proves build for this task only: not lint, and not another task.
	subject := evidence.Subject{
		Change: change, TaskID: taskID, AttemptID: attempt.AttemptID,
		HEAD: built.Production.HEAD, TaskHash: built.Production.TaskHash,
		ApprovalHash: built.Production.ApprovalHash, StateRevision: built.Production.StateRevision,
	}
	if built.Production.Matches(evidence.ProductionSubject{
		Subject: subject, Check: evidence.RequiredCheck{Class: evidence.ClassLint, CheckID: "go-vet"},
		PolicyDigest: built.Production.PolicyDigest,
	}) {
		t.Fatal("build evidence satisfied lint")
	}

	// Review is authored, never executed, and never by the recording actor.
	reviewed, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: agent, Profile: "production", Class: evidence.ClassReview,
		CheckID: "change-review", Reviewer: "human@example.com", ReviewPassed: true,
	})
	if err != nil || reviewed.Production == nil || !reviewed.Production.Passed ||
		reviewed.Production.CommandHash != "" || reviewed.Production.Reviewer != "human@example.com" {
		t.Fatalf("review verify = %#v, %v", reviewed, err)
	}
	if _, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: "human@example.com", Profile: "production", Class: evidence.ClassReview,
		CheckID: "change-review", Reviewer: "human@example.com", ReviewPassed: true,
	}); err == nil {
		t.Fatal("an actor recorded its own review")
	}

	// An interrupted runnable check records a bounded failure, never a pass.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	stopped, err := Verify(canceled, root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: agent, Profile: "production", Class: evidence.ClassLint,
		CheckID: "go-vet", Command: "go vet ./...",
	})
	if err != nil || stopped.Production == nil || stopped.Production.Passed ||
		!stopped.Production.Interrupted {
		t.Fatalf("interrupted verify = %#v, %v", stopped, err)
	}

	// The default profile is unchanged: test-run payload, no production record.
	observed, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{
		Actor: agent,
	})
	if err != nil || observed.Production != nil || observed.Evidence.Class != evidence.ClassTestRun ||
		!observed.Evidence.Passed {
		t.Fatalf("default verify = %#v, %v", observed, err)
	}
}

func TestProductionEvidenceRefusesUnknownSelection(t *testing.T) {
	const (
		change = "sample-loop"
		taskID = "edit-sample"
		agent  = "agent:builder"
	)
	root, attempt := productionAttempt(t, change, taskID, agent)
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	before, _ := os.ReadFile(evidencePath)
	for name, options := range map[string]VerifyOptions{
		"unknown profile": {Actor: agent, Profile: "prod", Class: evidence.ClassBuild,
			CheckID: "compile", Command: "go build ./..."},
		"default profile": {Actor: agent, Class: evidence.ClassBuild,
			CheckID: "compile", Command: "go build ./..."},
		"undeclared class": {Actor: agent, Profile: "production", Class: "scan",
			CheckID: "secrets", Command: "go build ./..."},
		"undeclared check": {Actor: agent, Profile: "production", Class: evidence.ClassBuild,
			CheckID: "go-vet", Command: "go build ./..."},
		"runnable without command": {Actor: agent, Profile: "production",
			Class: evidence.ClassBuild, CheckID: "compile"},
		"runnable with reviewer": {Actor: agent, Profile: "production", Class: evidence.ClassBuild,
			CheckID: "compile", Command: "go build ./...", Reviewer: "human@example.com"},
		"review with command": {Actor: agent, Profile: "production", Class: evidence.ClassReview,
			CheckID: "change-review", Reviewer: "human@example.com", Command: "go build ./..."},
		"review without reviewer": {Actor: agent, Profile: "production",
			Class: evidence.ClassReview, CheckID: "change-review", ReviewPassed: true},
		"production options without a class": {Actor: agent, Profile: "production",
			CheckID: "compile", Command: "go build ./..."},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, options)
			if err == nil {
				t.Fatalf("accepted: %#v", result)
			}
			after, _ := os.ReadFile(evidencePath)
			if string(after) != string(before) {
				t.Fatal("a refused verification wrote evidence")
			}
		})
	}
}

// productionAttempt drives the base loop to one active attempt, so production
// verification is proven on exactly the same canonical inputs as stage 5.
func productionAttempt(t *testing.T, change, taskID, agent string) (string, StartResult) {
	t.Helper()
	root := loopFixture(t)
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, change, "tester", "sample"); err != nil {
		t.Fatal(err)
	}
	loopArtifacts(t, root, change, taskID)
	if _, err := Approve(root, change, ApproveOptions{
		Approver: "human@example.com", Reason: "production proof",
		Input: nonTerminal(t), Output: os.Stderr,
	}); err != nil {
		t.Fatal(err)
	}
	loopGit(t, root, []string{"add", "-A"}, []string{"commit", "-m", "plan", "--no-gpg-sign"})
	manifest, err := Context(root, change, taskID, 0)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := Start(root, change, taskID, manifest.StateRevision, StartOptions{Actor: agent})
	if err != nil {
		t.Fatal(err)
	}
	return root, attempt
}
