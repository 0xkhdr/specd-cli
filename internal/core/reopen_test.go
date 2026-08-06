package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestReopenApprovedChangeRevokesExecutionAndRetainsRecords(t *testing.T) {
	root, attempt := startedAttemptRoot(t)
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	observed, err := record.New(record.Record{
		Family: record.FamilyEvidence, Kind: record.KindObserved, Change: "safe-change",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Actor: "agent:builder",
		Payload: json.RawMessage(`{"observation":"retained"}`),
	})
	if err != nil || record.Append(evidencePath, record.FamilyEvidence, observed) != nil {
		t.Fatal(err)
	}
	beforeEvidence, _ := os.ReadFile(evidencePath)
	mustWriteScopeFile(t, filepath.Join(root, "internal", "sample.go"), "package internal\n")
	runAttemptGit(t, root, "add", "internal/sample.go")
	runAttemptGit(t, root, "commit", "-m", "attempt work", "--no-gpg-sign")

	result, err := Reopen(root, "safe-change", ReopenIntent{
		ExpectedRevision: 3, Actor: "agent:builder", Reason: "repair the approved plan",
	})
	if err != nil || result.AttemptID != attempt.ID || result.ApprovalID == "" || result.HistoryID == "" {
		t.Fatalf("reopen = %#v, %v", result, err)
	}
	current := readReopenState(t, root)
	if current.Stage != "planning" || current.Revision != 4 || len(current.Tasks) != 0 ||
		current.Extensions[attemptExtensionKey] != nil || current.Extensions[completionExtensionKey] != nil ||
		current.LastTransition != result.HistoryID {
		t.Fatalf("reopened state = %#v", current)
	}
	afterEvidence, _ := os.ReadFile(evidencePath)
	if string(afterEvidence) != string(beforeEvidence) {
		t.Fatal("reopen changed prior evidence")
	}
	items, diagnostics, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil || len(diagnostics) != 0 || items[len(items)-1].Kind != record.KindReopened {
		t.Fatalf("history = %#v/%#v, %v", items, diagnostics, err)
	}
	if _, err := Approve(root, "safe-change", ApproveIntent{Route: ApprovalRouteAgentCapable}); !failure.IsCode(err, "human_approval_required") {
		t.Fatalf("agent approval after reopen = %v", err)
	}
}

func TestReopenAcceptsStaleApprovalAndRecoversInterruptedWrite(t *testing.T) {
	root, _ := startedAttemptRoot(t)
	appendAttemptFile(t, filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md"), []byte("\nstale\n"))
	intent := ReopenIntent{ExpectedRevision: 3, Actor: "agent:builder", Reason: "repair stale plan",
		AfterHistory: func() error { return errors.New("interrupted") }}
	if _, err := Reopen(root, "safe-change", intent); !failure.IsCode(err, "reopen_interrupted") {
		t.Fatalf("interrupted reopen = %v", err)
	}
	if current := readReopenState(t, root); current.Stage != "approved" || current.Revision != 3 {
		t.Fatalf("interrupted state = %#v", current)
	}
	intent.AfterHistory = nil
	if _, err := Reopen(root, "safe-change", intent); err != nil {
		t.Fatal(err)
	}
	items, _, _ := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	count := 0
	for _, item := range items {
		if item.Kind == record.KindReopened {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("recovery appended %d reopened records", count)
	}
}

func TestReopenRefusalsDoNotMutateManagedBytes(t *testing.T) {
	tests := []struct {
		name     string
		edit     func(*testing.T, string)
		revision uint64
		code     string
	}{
		{"stale", func(*testing.T, string) {}, 2, "stale_revision"},
		{"dirty", func(t *testing.T, root string) { mustWriteScopeFile(t, filepath.Join(root, "dirty.txt"), "dirty\n") }, 3, "attempt_dirty"},
		{"reconciling", func(t *testing.T, root string) { rewriteReopenStage(t, root, "reconciling") }, 3, "reopen_lifecycle"},
		{"archived", func(t *testing.T, root string) { rewriteReopenStage(t, root, "archived") }, 3, "reopen_lifecycle"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _ := startedAttemptRoot(t)
			test.edit(t, root)
			statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
			historyPath := filepath.Join(root, ".specd", "history.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeHistory, _ := os.ReadFile(historyPath)
			_, err := Reopen(root, "safe-change", ReopenIntent{ExpectedRevision: test.revision, Actor: "agent", Reason: "repair"})
			if !failure.IsCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
			afterState, _ := os.ReadFile(statePath)
			afterHistory, _ := os.ReadFile(historyPath)
			if string(beforeState) != string(afterState) || string(beforeHistory) != string(afterHistory) {
				t.Fatal("refusal changed managed bytes")
			}
		})
	}
	root, _ := startedAttemptRoot(t)
	if _, err := Reopen(root, "safe-change", ReopenIntent{ExpectedRevision: 3, Actor: "agent", Reason: "repair"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Reopen(root, "safe-change", ReopenIntent{ExpectedRevision: 4, Actor: "agent", Reason: "repair"}); !failure.IsCode(err, "reopen_lifecycle") {
		t.Fatalf("planning reopen = %v", err)
	}
}

func TestReopenConcurrentRevisionOneWinner(t *testing.T) {
	root, _ := startedAttemptRoot(t)
	var successes atomic.Int32
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := Reopen(root, "safe-change", ReopenIntent{ExpectedRevision: 3, Actor: "agent", Reason: "repair"}); err == nil {
				successes.Add(1)
			} else {
				errorsSeen <- err
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	if successes.Load() != 1 {
		t.Fatalf("successes = %d", successes.Load())
	}
	for err := range errorsSeen {
		if !failure.IsCode(err, "stale_revision") {
			t.Fatalf("loser = %v", err)
		}
	}
}

func readReopenState(t *testing.T, root string) state.State {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := state.Decode(raw, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func rewriteReopenStage(t *testing.T, root, stage string) {
	t.Helper()
	current := readReopenState(t, root)
	current.Stage = stage
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
