package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestAttemptCleanFrontierPersistsExactBinding(t *testing.T) {
	root := attemptRoot(t)
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	if attempt.ID == "" || attempt.TaskID != "T1" || attempt.RevisionBefore != 2 ||
		attempt.RevisionAfter != 3 || attempt.Assurance != AttemptAssurance ||
		attempt.ContractHash == "" || attempt.ApprovalHash == "" || attempt.BaselineHEAD == "" ||
		len(attempt.DeclaredFiles) != 1 || attempt.DeclaredFiles[0] != "internal/sample.go" {
		t.Fatalf("attempt = %#v", attempt)
	}
	current := readTaskTransitionState(t, root)
	reloaded, found, err := CurrentAttempt(current, "T1")
	var kept map[string]int
	keepErr := json.Unmarshal(current.Extensions["keep"], &kept)
	if err != nil || !found || keepErr != nil || reloaded.ID != attempt.ID || kept["value"] != 1 ||
		current.Revision != 3 || current.LastTransition == "" {
		t.Fatalf("reloaded attempt/state = %#v / %#v / %v", reloaded, current, err)
	}
	activity, err := ProjectTaskActivity(current.Tasks, "T1")
	if err != nil || activity != TaskInProgress {
		t.Fatalf("activity = %q, %v", activity, err)
	}
	history, diagnostics, err := record.Replay(
		filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
	)
	if err != nil || len(diagnostics) != 0 ||
		history[len(history)-1].Kind != record.KindAttempt ||
		history[len(history)-1].ID != current.LastTransition {
		t.Fatalf("history = %#v, %#v, %v", history, diagnostics, err)
	}
}

func TestStartHarnessEntryMintsSealedAuthority(t *testing.T) {
	root := attemptRoot(t)
	attempt, err := StartTaskAttempt(root, "safe-change", StartAttemptIntent{
		TaskID: "T1", ExpectedRevision: 2, Actor: "agent:builder",
	})
	if err != nil || attempt.ID == "" || attempt.RevisionBefore != 2 ||
		attempt.RevisionAfter != 3 || attempt.Assurance != AttemptAssurance {
		t.Fatalf("start = %#v, %v", attempt, err)
	}
	if _, err := StartTaskAttempt(root, "safe-change", StartAttemptIntent{
		TaskID: "T1", ExpectedRevision: 3, Actor: "agent:builder",
	}); !failure.IsCode(err, "attempt_exists") {
		t.Fatalf("repeated start = %v", err)
	}
}

func TestStartHarnessEntryRejectsMissingActor(t *testing.T) {
	root := attemptRoot(t)
	if _, err := StartTaskAttempt(root, "safe-change", StartAttemptIntent{
		TaskID: "T1", ExpectedRevision: 2,
	}); !failure.IsCode(err, "attempt_unauthorized") {
		t.Fatalf("unauthorized start = %v", err)
	}
}

func TestAttemptConcurrentCASOneWinner(t *testing.T) {
	root := attemptRoot(t)
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := StartAttempt(root, "safe-change", attemptRequest(2)); err == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful attempts = %d", successes.Load())
	}
}

func TestAttemptInterruptedRecoversExactlyOnce(t *testing.T) {
	root := attemptRoot(t)
	request := attemptRequest(2)
	request.AfterHistory = func() error { return errors.New("interrupted") }
	if _, err := StartAttempt(root, "safe-change", request); !failure.IsCode(err, "attempt_interrupted") {
		t.Fatalf("interruption = %v", err)
	}
	current := readTaskTransitionState(t, root)
	if current.Revision != 2 || current.Extensions[attemptExtensionKey] != nil ||
		len(current.Tasks) != 0 {
		t.Fatalf("partial attempt became usable: %#v", current)
	}
	request.AfterHistory = nil
	if _, err := StartAttempt(root, "safe-change", request); err != nil {
		t.Fatal(err)
	}
	history, _, err := record.Replay(
		filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
	)
	attempts := 0
	for _, item := range history {
		if item.Kind == record.KindAttempt {
			attempts++
		}
	}
	if err != nil || attempts != 1 {
		t.Fatalf("attempt history count = %d, %v", attempts, err)
	}
}

func TestAttemptAdmissionRefusalsPersistNothing(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*testing.T) string
		mutate func(*testing.T, string)
		req    AttemptRequest
		code   string
	}{
		{"dirty", attemptRoot, func(t *testing.T, root string) {
			appendAttemptFile(t, filepath.Join(root, "README.md"), []byte("dirty\n"))
		}, attemptRequest(2), "attempt_dirty"},
		{"commitless", commitlessAttemptRoot, nil, attemptRequest(2), "attempt_commitless"},
		{"stale", attemptRoot, nil, attemptRequest(1), "stale_revision"},
		{"stale approval", attemptRoot, func(t *testing.T, root string) {
			appendAttemptFile(t,
				filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md"),
				[]byte("\ndrift\n"),
			)
		}, attemptRequest(2), "attempt_not_ready"},
		{"unauthorized", attemptRoot, nil, AttemptRequest{
			TaskID: "T1", ExpectedRevision: 2,
		}, "attempt_unauthorized"},
		{"managed", attemptRoot, func(t *testing.T, root string) {
			rewriteAttemptTasks(t, root, ".specd/owned")
		}, attemptRequest(2), "attempt_scope"},
		{"escaping", attemptRoot, func(t *testing.T, root string) {
			rewriteAttemptTasks(t, root, "../escape")
		}, attemptRequest(2), "plan_invalid"},
		{"managed hardlink", attemptRoot, func(t *testing.T, root string) {
			target := filepath.Join(root, "internal", "sample.go")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"), target); err != nil {
				t.Skipf("hardlinks unavailable: %v", err)
			}
		}, attemptRequest(2), "attempt_scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := test.setup(t)
			if test.mutate != nil {
				test.mutate(t, root)
			}
			statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
			historyPath := filepath.Join(root, ".specd", "history.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeHistory, _ := os.ReadFile(historyPath)
			_, err := StartAttempt(root, "safe-change", test.req)
			if !failure.IsCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
			afterState, _ := os.ReadFile(statePath)
			afterHistory, _ := os.ReadFile(historyPath)
			if string(beforeState) != string(afterState) || string(beforeHistory) != string(afterHistory) {
				t.Fatal("refusal persisted attempt data")
			}
		})
	}
}

func TestAttemptIgnoredWorktreeFileDoesNotBlockStart(t *testing.T) {
	root := attemptRoot(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("coverage.out\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "coverage.out"), []byte("ignored build output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StartAttempt(root, "safe-change", attemptRequest(2)); err != nil {
		t.Fatalf("ignored file blocked start: %v", err)
	}
}

func attemptRoot(t *testing.T) string {
	t.Helper()
	root := approvedStatusRoot(t)
	current := readTaskTransitionState(t, root)
	if current.Extensions == nil {
		current.Extensions = map[string]json.RawMessage{}
	}
	current.Extensions["keep"] = json.RawMessage(`{"value":1}`)
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".specd", "changes", "safe-change", "state.json"), raw, 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeAttemptGitignore(t, root)
	initAttemptGit(t, root, true)
	return root
}

func commitlessAttemptRoot(t *testing.T) string {
	t.Helper()
	root := approvedStatusRoot(t)
	writeAttemptGitignore(t, root)
	initAttemptGit(t, root, false)
	return root
}

func writeAttemptGitignore(t *testing.T, root string) {
	t.Helper()
	raw := []byte(".specd/history.jsonl\n.specd/evidence.jsonl\n.specd/changes/*/state.json\n")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func initAttemptGit(t *testing.T, root string, commit bool) {
	t.Helper()
	runAttemptGit(t, root, "init")
	runAttemptGit(t, root, "config", "user.email", "test@example.com")
	runAttemptGit(t, root, "config", "user.name", "Test")
	if commit {
		runAttemptGit(t, root, "add", ".")
		runAttemptGit(t, root, "commit", "-m", "approved baseline", "--no-gpg-sign")
	}
}

func runAttemptGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func appendAttemptFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		t.Fatal(err)
	}
}

func rewriteAttemptTasks(t *testing.T, root, file string) {
	t.Helper()
	raw := []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n" +
			"|---|---|---|---|---|---|---|\n" +
			"| T1 | builder | " + file + " | | sample/Requirement: Stable updates | `go test ./...` | Check passes |\n",
	)
	if err := os.WriteFile(
		filepath.Join(root, ".specd", "changes", "safe-change", "tasks.md"), raw, 0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func attemptRequest(revision uint64) AttemptRequest {
	return AttemptRequest{
		TaskID: "T1", Authority: trustedTaskTransitionAuthority("agent:builder"),
		ExpectedRevision: revision,
	}
}
