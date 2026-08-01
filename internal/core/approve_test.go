package core

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestApproveIdentity(t *testing.T) {
	if got, err := ResolveApprovalIdentity("human@example.com", ""); err != nil || got != "human@example.com" {
		t.Fatalf("identity = %q, %v", got, err)
	}
	for _, pair := range [][2]string{{"", ""}, {"a@example.com", "b@example.com"}} {
		if _, err := ResolveApprovalIdentity(pair[0], pair[1]); err == nil {
			t.Fatalf("accepted identity sources %#v", pair)
		}
	}
}

func TestApproveAgentCapableRouteRefusesBeforeFilesystem(t *testing.T) {
	for _, route := range []ApprovalRoute{"", ApprovalRouteAgentCapable, "future"} {
		_, err := Approve("/does/not/exist", "safe-change", ApproveIntent{
			GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
			Reason: "reviewed", Route: route,
		})
		if !failure.IsCode(err, "human_approval_required") {
			t.Fatalf("route %q reached filesystem: %v", route, err)
		}
	}
}

func TestApproveTransaction(t *testing.T) {
	root := checkRoot(t, true)
	evidence := filepath.Join(root, ".specd", "evidence.jsonl")
	beforeEvidence, _ := os.ReadFile(evidence)
	intent := ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
		Reason: "reviewed exact planning bytes", Route: ApprovalRouteHumanTerminal,
	}
	approval, err := Approve(root, "safe-change", intent)
	if err != nil {
		t.Fatal(err)
	}
	if approval.RevisionBefore != 1 || approval.RevisionAfter != 2 {
		t.Fatalf("approval revisions = %#v", approval)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"))
	current, err := state.Decode(raw, "safe-change")
	if err != nil || current.Stage != "approved" || current.Revision != 2 || len(current.Approvals) != 1 {
		t.Fatalf("state = %#v, %v", current, err)
	}
	history, _, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil || len(history) != 1 || history[0].Kind != record.KindApproved {
		t.Fatalf("history = %#v, %v", history, err)
	}
	afterEvidence, _ := os.ReadFile(evidence)
	if string(beforeEvidence) != string(afterEvidence) {
		t.Fatal("approval created evidence")
	}
	if _, err := Approve(root, "safe-change", intent); err == nil {
		t.Fatal("second approval advanced approved state")
	}
}

func TestApproveTransactionRace(t *testing.T) {
	root := checkRoot(t, true)
	intent := ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	}
	var successes atomic.Int32
	var wait sync.WaitGroup
	refusals := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := Approve(root, "safe-change", intent); err == nil {
				successes.Add(1)
			} else {
				refusals <- err
			}
		}()
	}
	wait.Wait()
	close(refusals)
	if successes.Load() != 1 {
		t.Fatalf("successful approvals = %d", successes.Load())
	}
	// The loser needs the next legal lifecycle action, never an authoring repair
	// for findings that are not defects.
	for err := range refusals {
		var refusal *failure.Refusal
		if !errors.As(err, &refusal) || refusal.Code != "lifecycle_transition" ||
			refusal.Next != "continue with the next legal lifecycle action" {
			t.Fatalf("loser refusal = %#v", err)
		}
	}
}

func TestApproveTransactionInterruptedRecovers(t *testing.T) {
	root := checkRoot(t, true)
	intent := ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed",
		Route:        ApprovalRouteHumanTerminal,
		AfterHistory: func() error { return errors.New("interrupted") },
	}
	if _, err := Approve(root, "safe-change", intent); err == nil {
		t.Fatal("injected interruption succeeded")
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"))
	current, _ := state.Decode(raw, "safe-change")
	if current.Stage != "planning" || len(current.Approvals) != 0 {
		t.Fatalf("partial approval became usable: %#v", current)
	}
	intent.AfterHistory = nil
	if _, err := Approve(root, "safe-change", intent); err != nil {
		t.Fatal(err)
	}
	history, _, _ := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if len(history) != 1 {
		t.Fatalf("recovery appended %d records", len(history))
	}
}

func TestApproveInterruptedRecoveryRefusesDifferentApprover(t *testing.T) {
	root := checkRoot(t, true)
	first := ApproveIntent{
		GitEmail: "first@example.com", ClaimedApprover: "first@example.com", Reason: "reviewed",
		Route:        ApprovalRouteHumanTerminal,
		AfterHistory: func() error { return errors.New("interrupted") },
	}
	if _, err := Approve(root, "safe-change", first); err == nil {
		t.Fatal("injected interruption succeeded")
	}
	second := ApproveIntent{
		GitEmail: "second@example.com", ClaimedApprover: "second@example.com",
		Reason: "also reviewed", Route: ApprovalRouteHumanTerminal,
	}
	if _, err := Approve(root, "safe-change", second); !failure.IsCode(err, "approval_recovery") {
		t.Fatalf("foreign recovery = %v", err)
	}
	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	raw, _ := os.ReadFile(statePath)
	current, _ := state.Decode(raw, "safe-change")
	if current.Stage != "planning" || len(current.Approvals) != 0 {
		t.Fatalf("foreign recovery advanced lifecycle: %#v", current)
	}
	history, _, _ := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if len(history) != 1 {
		t.Fatalf("foreign recovery appended %d records", len(history))
	}
	first.AfterHistory = nil
	approval, err := Approve(root, "safe-change", first)
	if err != nil || approval.Approver != "first@example.com" || approval.Reason != "reviewed" {
		t.Fatalf("original approver recovery = %#v, %v", approval, err)
	}
	history, _, _ = record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if len(history) != 1 {
		t.Fatalf("recovery appended %d records", len(history))
	}
}

func TestApproveTransactionFailedGatesCommitNothing(t *testing.T) {
	root := checkRoot(t, false)
	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	before, _ := os.ReadFile(statePath)
	_, err := Approve(root, "safe-change", ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	})
	if err == nil {
		t.Fatal("failed gates approved")
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("failed gates changed state")
	}
}
