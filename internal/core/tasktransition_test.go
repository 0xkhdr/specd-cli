package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestTaskTransitionCommitsHistoryAndState(t *testing.T) {
	root := approvedStatusRoot(t)
	request := taskStartRequest(2)
	transition, err := TransitionTaskActivity(root, "safe-change", request)
	if err != nil {
		t.Fatal(err)
	}
	if transition.TaskID != "T1" || transition.From != TaskPending || transition.To != TaskInProgress {
		t.Fatalf("transition = %#v", transition)
	}
	current := readTaskTransitionState(t, root)
	activity, err := ProjectTaskActivity(current.Tasks, "T1")
	if err != nil || activity != TaskInProgress || current.Revision != 3 {
		t.Fatalf("state = %#v, activity = %q, %v", current, activity, err)
	}
	history, diagnostics, err := record.Replay(
		filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
	)
	if err != nil || len(diagnostics) != 0 ||
		history[len(history)-1].Kind != record.KindTaskTransition ||
		current.LastTransition != history[len(history)-1].ID {
		t.Fatalf("history/state = %#v / %#v / %v", history, current, err)
	}
}

func TestTaskTransitionConcurrentRevisionOneWinner(t *testing.T) {
	root := approvedStatusRoot(t)
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := TransitionTaskActivity(root, "safe-change", taskStartRequest(2)); err == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful transitions = %d", successes.Load())
	}
	current := readTaskTransitionState(t, root)
	if current.Revision != 3 {
		t.Fatalf("revision = %d", current.Revision)
	}
}

func TestTaskTransitionActiveSiblingKeepsCurrentApproval(t *testing.T) {
	root := checkRoot(t, true)
	writeCheckFile(t, filepath.Join(root, ".specd", "changes", "safe-change", "tasks.md"), []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| T1 | builder | internal/one.go | | sample/Requirement: Stable updates | `go test ./...` | One works |\n"+
			"| T2 | builder | internal/two.go | | sample/Requirement: Stable updates | `go test ./...` | Two works |\n",
	))
	_, err := Approve(root, "safe-change", ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
		Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionTaskActivity(root, "safe-change", taskStartRequest(2)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadReadinessSnapshot(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := snapshot.Model().Task("T1")
	if !snapshot.Approval().Current || first.Readiness != ReadinessActive ||
		!reflect.DeepEqual(snapshot.Model().Frontier(), []string{"T2"}) {
		t.Fatalf("active sibling snapshot = %#v / %#v", snapshot.Approval(), snapshot.Model().Tasks())
	}
}

func TestTaskTransitionRefusalsChangeNothing(t *testing.T) {
	tests := []struct {
		name    string
		root    func(*testing.T) string
		request TaskTransitionRequest
		code    string
	}{
		{"unauthorized", approvedStatusRoot, TaskTransitionRequest{
			TaskID: "T1", To: TaskInProgress, ExpectedRevision: 2,
		}, "task_unauthorized"},
		{"stale", approvedStatusRoot, taskStartRequest(1), "stale_revision"},
		{"unknown task", approvedStatusRoot, TaskTransitionRequest{
			TaskID: "missing", To: TaskInProgress, Authority: trustedTaskTransitionAuthority("agent"), ExpectedRevision: 2,
		}, "task_unknown"},
		{"impossible", approvedStatusRoot, TaskTransitionRequest{
			TaskID: "T1", To: TaskFailed, Authority: trustedTaskTransitionAuthority("agent"), ExpectedRevision: 2,
		}, "task_transition"},
		{"invalid plan", func(t *testing.T) string { return checkRoot(t, false) }, TaskTransitionRequest{
			TaskID: "T1", To: TaskInProgress, Authority: trustedTaskTransitionAuthority("agent"), ExpectedRevision: 1,
		}, "plan_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := test.root(t)
			statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
			historyPath := filepath.Join(root, ".specd", "history.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeHistory, _ := os.ReadFile(historyPath)
			_, err := TransitionTaskActivity(root, "safe-change", test.request)
			assertActionableRefusal(t, err, test.code)
			afterState, _ := os.ReadFile(statePath)
			afterHistory, _ := os.ReadFile(historyPath)
			if string(beforeState) != string(afterState) || string(beforeHistory) != string(afterHistory) {
				t.Fatal("refusal changed state or history")
			}
		})
	}
}

func TestTaskTransitionInterruptedRecoversExactlyOnce(t *testing.T) {
	root := approvedStatusRoot(t)
	request := taskStartRequest(2)
	request.AfterHistory = func() error { return errors.New("interrupted") }
	if _, err := TransitionTaskActivity(root, "safe-change", request); !failure.IsCode(err, "task_interrupted") {
		t.Fatalf("interruption = %v", err)
	}
	current := readTaskTransitionState(t, root)
	if current.Revision != 2 || len(current.Tasks) != 0 {
		t.Fatalf("partial transition became usable: %#v", current)
	}
	request.AfterHistory = nil
	if _, err := TransitionTaskActivity(root, "safe-change", request); err != nil {
		t.Fatal(err)
	}
	history, _, err := record.Replay(
		filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
	)
	if err != nil || len(history) != 2 || history[1].Kind != record.KindTaskTransition {
		t.Fatalf("recovery history = %#v, %v", history, err)
	}
}

func TestTaskTransitionCompletionRequiresStageFiveEvidence(t *testing.T) {
	root := approvedStatusRoot(t)
	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	current := readTaskTransitionState(t, root)
	current.Tasks["T1"] = json.RawMessage(`"in_progress"`)
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = TransitionTaskActivity(root, "safe-change", TaskTransitionRequest{
		TaskID: "T1", To: TaskCompleted, Authority: trustedTaskTransitionAuthority("agent"), ExpectedRevision: 2,
	})
	if !failure.IsCode(err, "task_evidence_required") {
		t.Fatalf("completion without evidence = %v", err)
	}
}

func taskStartRequest(revision uint64) TaskTransitionRequest {
	return TaskTransitionRequest{
		TaskID: "T1", To: TaskInProgress, Authority: trustedTaskTransitionAuthority("agent:builder"),
		ExpectedRevision: revision,
	}
}

func readTaskTransitionState(t *testing.T, root string) state.State {
	t.Helper()
	path := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := state.Decode(raw, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	return current
}
