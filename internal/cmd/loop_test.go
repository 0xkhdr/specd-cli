package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// TestLoopCompletesOneTaskWithRealEvidence drives the whole base loop
// against one throwaway project that is both a Git
// worktree and a Go module, so `start` finds a clean tracked baseline and
// `verify` runs a real `go test` in it. It is the acceptance proof that a task
// can be completed at all, and the regression guard for plain `go test -run`
// output being read as a real (non-zero-match) observation.
func TestLoopCompletesOneTaskWithRealEvidence(t *testing.T) {
	const (
		change   = "sample-loop"
		taskID   = "edit-sample"
		agent    = "agent:builder"
		approver = "human@example.com"
	)
	root := loopFixture(t)

	if _, err := Init(root); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := New(root, change, "tester", "sample"); err != nil {
		t.Fatalf("new: %v", err)
	}

	t.Logf("init+new: %s", change)
	// 1. Scaffolded placeholders must not be approvable.
	scaffolded, err := Check(root, change)
	if err != nil {
		t.Fatalf("check scaffold: %v", err)
	}
	if scaffolded.Success || len(scaffolded.Findings) == 0 {
		t.Fatalf("scaffolded plan checked clean: %#v", scaffolded)
	}

	t.Logf("check scaffold: success=%v findings=%d", scaffolded.Success, len(scaffolded.Findings))
	loopArtifacts(t, root, change, taskID)
	authored, err := Check(root, change)
	if err != nil {
		t.Fatalf("check authored: %v", err)
	}
	if !authored.Success {
		t.Fatalf("authored plan failed check:\n%s", RenderCheck(authored))
	}

	t.Logf("check authored: success=%v", authored.Success)
	// 2. Human approval through the non-interactive route; the trusted identity
	// is the fixture's own git user.email.
	stdin := nonTerminal(t)
	if _, err := Approve(root, change, ApproveOptions{
		Approver: approver, Reason: "loop proof", Input: stdin, Output: os.Stderr,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// Commit the approved plan so the worktree is clean for start.
	loopGit(t, root, []string{"add", "-A"}, []string{"commit", "-m", "plan", "--no-gpg-sign"})

	t.Logf("approve: %s", approver)
	next, err := Next(root, change, taskID)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if next.Selected == nil || next.Classification != "selected" {
		t.Fatalf("next = %#v", next)
	}
	t.Logf("next: frontier=%v selected=%s", next.Frontier, next.Selected.ID)
	manifest, err := Context(root, change, taskID, 0)
	if err != nil {
		t.Fatalf("context: %v", err)
	}

	t.Logf("context: revision=%d", manifest.StateRevision)
	attempt, err := Start(root, change, taskID, manifest.StateRevision, StartOptions{Actor: agent})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(attempt.DeclaredFiles) != 1 || attempt.DeclaredFiles[0] != "sample.go" {
		t.Fatalf("declared files = %#v", attempt.DeclaredFiles)
	}

	t.Logf("start: attempt=%s head=%s revision %d->%d declared=%v", attempt.AttemptID[:8],
		attempt.BaselineHEAD[:8], attempt.RevisionBefore, attempt.RevisionAfter, attempt.DeclaredFiles)
	// 3. Edit exactly the declared file: a tracked M path inside scope.
	if err := os.WriteFile(filepath.Join(root, "sample.go"),
		[]byte("package sample\n\nfunc Sample() string { return \"after\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 4. Completion refuses while no evidence exists, even though every other
	// guard passes.
	if _, err := Complete(root, change, taskID, attempt.RevisionAfter, CompleteOptions{Actor: agent}); err == nil {
		t.Fatal("completion succeeded without evidence")
	} else if !failure.IsCode(err, "complete_evidence") {
		t.Fatalf("no-evidence refusal = %v", err)
	}

	t.Log("complete without evidence: refused complete_evidence")
	// 5. Real command execution.
	verified, err := Verify(context.Background(), root, change, taskID, attempt.AttemptID, VerifyOptions{Actor: agent})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	run := verified.Evidence
	if !run.Passed || !run.NonVacuous || run.ZeroMatch || run.ExitCode != 0 {
		t.Fatalf("evidence = %#v", run)
	}
	t.Logf("verify: exit=%d passed=%v non_vacuous=%v zero_match=%v evidence=%s",
		run.ExitCode, run.Passed, run.NonVacuous, run.ZeroMatch, verified.RecordID)
	// Verify observes; it never completes.
	if verified.Complete {
		t.Fatal("verify reported completion")
	}
	if activity := loopActivity(t, root, change, taskID); activity != core.TaskInProgress {
		t.Fatalf("activity after verify = %q", activity)
	}

	// 6. Completion consumes that exact evidence once.
	completion, err := Complete(root, change, taskID, attempt.RevisionAfter, CompleteOptions{Actor: agent})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completion.EvidenceID != verified.RecordID ||
		completion.RevisionAfter != attempt.RevisionAfter+1 {
		t.Fatalf("completion = %#v (evidence %q)", completion, verified.RecordID)
	}
	t.Logf("complete: evidence=%s revision %d->%d", completion.EvidenceID,
		completion.RevisionBefore, completion.RevisionAfter)
	status, err := Status(root, change)
	if err != nil {
		t.Fatal(err)
	}
	if status.Revision != completion.RevisionAfter {
		t.Fatalf("revision = %d, want %d", status.Revision, completion.RevisionAfter)
	}
	if activity := loopActivity(t, root, change, taskID); activity != core.TaskCompleted {
		t.Fatalf("activity after complete = %q", activity)
	}

	// 7. Replay refuses: the evidence is spent and the task is no longer active.
	if _, err := Complete(root, change, taskID, completion.RevisionAfter, CompleteOptions{Actor: agent}); err == nil {
		t.Fatal("second completion succeeded")
	} else if !failure.IsCode(err, "complete_activity") {
		t.Fatalf("replay refusal = %v", err)
	} else {
		t.Logf("second complete: refused %v", err)
	}
}

// loopFixture returns a project root that is a Git worktree with one commit and
// a Go module holding the file the task will edit plus the test that proves it.
func loopFixture(t *testing.T) string {
	t.Helper()
	root := tempRoot(t)
	for name, body := range map[string]string{
		"go.mod":         "module sample\n\ngo 1.26\n",
		"sample.go":      "package sample\n\nfunc Sample() string { return \"before\" }\n",
		"sample_test.go": "package sample\n\nimport \"testing\"\n\nfunc TestSampleLoop(t *testing.T) {\n\tif Sample() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loopGit(t, root,
		[]string{"init"},
		[]string{"config", "user.email", "human@example.com"},
		[]string{"config", "user.name", "Human"},
		[]string{"add", "."},
		[]string{"commit", "-m", "baseline", "--no-gpg-sign"},
	)
	return root
}

func loopGit(t *testing.T, root string, commands ...[]string) {
	t.Helper()
	for _, arguments := range commands {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, raw)
		}
	}
}

// loopArtifacts replaces every scaffolded placeholder with content that passes
// the planning gates, declaring exactly the one file the task edits.
func loopArtifacts(t *testing.T, root, change, taskID string) {
	t.Helper()
	dir := filepath.Join(root, ".specd", "changes", change)
	writeStatusFile(t, filepath.Join(dir, "proposal.md"), []byte(
		"## Problem\nSample greeting is stale.\n## Outcome\nSample greeting is current.\n"+
			"## Scope\nLocal sample greeting.\n## Non-goals\nGreeting translation.\n"+
			"## Affected capabilities\nsample\n"))
	writeStatusFile(t, filepath.Join(dir, "design.md"), []byte(
		"## Boundaries\nsample/Requirement: Current greeting\n## Interfaces\nOne exported greeting function.\n"+
			"## Invariants\nGreeting is never empty.\n## Failure behavior\nA failed edit leaves the greeting unchanged.\n"+
			"## Integration\nExisting sample package.\n## Alternatives\nA greeting table is unnecessary for one value.\n"+
			"## Owner\nsample\n"))
	writeStatusFile(t, filepath.Join(dir, "specs", "sample", "spec.md"), []byte(
		"## Purpose\nSample greeting truth.\n## ADDED Requirements\n### Requirement: Current greeting\n"+
			"The sample package MUST return a non-empty current greeting.\n#### Scenario: Greeting requested\n"+
			"- **WHEN** the greeting is requested\n- **THEN** a non-empty greeting is returned\n"))
	writeStatusFile(t, filepath.Join(dir, "tasks.md"), []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| "+taskID+" | builder | sample.go | | sample/Requirement: Current greeting | "+
			"`go test . -run '^TestSampleLoop$' -count=1` | The greeting is current and its test passes |\n"))
}

func loopActivity(t *testing.T, root, change, taskID string) core.TaskActivity {
	t.Helper()
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		t.Fatal(err)
	}
	row, found := snapshot.Model().Task(taskID)
	if !found {
		t.Fatalf("task %q is not canonical", taskID)
	}
	return row.Activity
}
