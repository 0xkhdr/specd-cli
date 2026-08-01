package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

var syncClock = time.Date(2026, 7, 30, 9, 30, 0, 0, time.UTC)

// syncReadyRoot returns a change whose only task is completed with current
// evidence under a current human approval: the exact state sync requires.
func syncReadyRoot(t *testing.T) string {
	t.Helper()
	root, _, _ := completeRoot(t, true)
	if _, err := CompleteTask(root, "safe-change", completeRequest(3)); err != nil {
		t.Fatal(err)
	}
	return root
}

func syncOptions() SyncOptions {
	return SyncOptions{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
		Reason: "reviewed", Route: ApprovalRouteHumanTerminal, Now: syncClock,
	}
}

func acceptedPath(root string) string {
	return filepath.Join(root, ".specd", "specs", "sample", "spec.md")
}

func syncState(t *testing.T, root string) state.State {
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

func historyKinds(t *testing.T, root string, kind record.Kind) []record.Record {
	t.Helper()
	items, diagnostics, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("history: %v %v", err, diagnostics)
	}
	var found []record.Record
	for _, item := range items {
		if item.Kind == kind {
			found = append(found, item)
		}
	}
	return found
}

func syncRefusal(t *testing.T, err error, code string) *failure.Refusal {
	t.Helper()
	var refusal *failure.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("expected refusal %s, got %v", code, err)
	}
	if refusal.Code != code {
		t.Fatalf("expected code %s, got %s (%s)", code, refusal.Code, refusal.Reason)
	}
	if strings.TrimSpace(refusal.Next) == "" {
		t.Fatal("every refusal names exactly one next action")
	}
	return refusal
}

func TestSyncCommitsAcceptedTruthOnce(t *testing.T) {
	root := syncReadyRoot(t)
	result, err := Sync(root, "safe-change", syncOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.NoOp || result.Approver != "human@example.com" || len(result.Capabilities) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.ArchiveTarget != "archive/2026-07-30-safe-change" {
		t.Fatalf("archive target = %s", result.ArchiveTarget)
	}
	raw, err := os.ReadFile(acceptedPath(root))
	if err != nil {
		t.Fatal(err)
	}
	accepted := string(raw)
	if !strings.Contains(accepted, "### Requirement: Stable updates") ||
		!strings.Contains(accepted, "## Purpose") {
		t.Fatalf("accepted spec = %q", accepted)
	}
	for _, heading := range []string{"ADDED", "MODIFIED", "REMOVED", "RENAMED"} {
		if strings.Contains(accepted, "## "+heading+" Requirements") {
			t.Fatalf("accepted truth kept a %s delta heading", heading)
		}
	}
	if current := syncState(t, root); current.Stage != "reconciling" ||
		current.Revision != result.RevisionAfter || current.LastTransition != result.HistoryID {
		t.Fatalf("state = %+v", current)
	}
	records := historyKinds(t, root, record.KindSynced)
	if len(records) != 1 {
		t.Fatalf("expected one sync record, got %d", len(records))
	}
	payload, err := record.DecodeSyncPayload(records[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	// History must identify the exact inputs, outputs, proof, and approver.
	if payload.Approver != "human@example.com" || payload.ActorClass != "human" ||
		payload.PlanHash != result.PlanHash || payload.EvidenceSet != result.EvidenceSet ||
		len(payload.Outputs) != 1 || payload.Outputs[0].Path != "specs/sample/spec.md" ||
		payload.Outputs[0].Before != "" || payload.Outputs[0].After == "" {
		t.Fatalf("sync payload = %+v", payload)
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	root := syncReadyRoot(t)
	first, err := Sync(root, "safe-change", syncOptions())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(acceptedPath(root))
	if err != nil {
		t.Fatal(err)
	}
	revision := syncState(t, root).Revision

	second, err := Sync(root, "safe-change", syncOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !second.NoOp || second.HistoryID != first.HistoryID {
		t.Fatalf("second sync = %+v", second)
	}
	after, err := os.ReadFile(acceptedPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a repeated sync rewrote accepted bytes")
	}
	if got := syncState(t, root).Revision; got != revision {
		t.Fatalf("revision moved from %d to %d on a no-op", revision, got)
	}
	if records := historyKinds(t, root, record.KindSynced); len(records) != 1 {
		t.Fatalf("a repeated sync appended %d records", len(records))
	}
}

func TestSyncRefusesTheAgentRoute(t *testing.T) {
	root := syncReadyRoot(t)
	options := syncOptions()
	options.Route = ApprovalRouteAgentCapable
	_, err := Sync(root, "safe-change", options)
	syncRefusal(t, err, "human_approval_required")
	if _, statErr := os.Stat(acceptedPath(root)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("an agent route reached accepted truth")
	}
}

func TestSyncRefusesUntrustedIdentity(t *testing.T) {
	root := syncReadyRoot(t)
	options := syncOptions()
	options.ClaimedApprover = "someone-else@example.com"
	_, err := Sync(root, "safe-change", options)
	syncRefusal(t, err, "approval_identity")

	options = syncOptions()
	options.Reason = "  "
	_, err = Sync(root, "safe-change", options)
	syncRefusal(t, err, "approval_intent")
}

func TestSyncRefusesIncompleteProof(t *testing.T) {
	t.Run("incomplete task", func(t *testing.T) {
		root, _, _ := completeRoot(t, true)
		_, err := Sync(root, "safe-change", syncOptions())
		refusal := syncRefusal(t, err, "sync_blocked")
		if !strings.Contains(refusal.Reason, "tasks_incomplete") {
			t.Fatalf("reason = %s", refusal.Reason)
		}
		if _, statErr := os.Stat(acceptedPath(root)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("an incomplete change reached accepted truth")
		}
	})

	t.Run("stale approval", func(t *testing.T) {
		root := syncReadyRoot(t)
		proposal := filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md")
		if err := os.WriteFile(proposal, []byte("## Problem\nedited after approval\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Sync(root, "safe-change", syncOptions())
		syncRefusal(t, err, "sync_blocked")
		if _, statErr := os.Stat(acceptedPath(root)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("a stale approval reached accepted truth")
		}
	})

	t.Run("missing evidence", func(t *testing.T) {
		root := syncReadyRoot(t)
		if err := os.WriteFile(filepath.Join(root, ".specd", "evidence.jsonl"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Sync(root, "safe-change", syncOptions())
		refusal := syncRefusal(t, err, "sync_blocked")
		if !strings.Contains(refusal.Reason, "evidence_stale") {
			t.Fatalf("reason = %s", refusal.Reason)
		}
	})
}

func TestSyncRefusesOutsideItsLifecycle(t *testing.T) {
	root := checkRoot(t, true)
	_, err := Sync(root, "safe-change", syncOptions())
	syncRefusal(t, err, "sync_lifecycle")
}

func TestSyncRecoversAnInterruptedCommit(t *testing.T) {
	for _, boundary := range []string{"after-stage", "after-manifest", "before-cleanup"} {
		t.Run(boundary, func(t *testing.T) {
			root := syncReadyRoot(t)
			options := syncOptions()
			options.Hook = func(step string) error {
				if step == boundary {
					return errors.New("injected interruption")
				}
				return nil
			}
			if _, err := Sync(root, "safe-change", options); err == nil {
				t.Fatalf("expected interruption at %s", boundary)
			}
			// The retry is the same call. Either it rolls the committed
			// transaction forward and observes a no-op, or the uncommitted
			// staging rolled back and it commits cleanly. Both leave exactly
			// one sync record and the planned accepted bytes.
			result, err := Sync(root, "safe-change", syncOptions())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(acceptedPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "### Requirement: Stable updates") {
				t.Fatalf("accepted spec = %q", raw)
			}
			if records := historyKinds(t, root, record.KindSynced); len(records) != 1 {
				t.Fatalf("recovery left %d sync records", len(records))
			}
			if current := syncState(t, root); current.Stage != "reconciling" {
				t.Fatalf("state = %+v", current)
			}
			if result.HistoryID == "" {
				t.Fatal("recovery lost the history identity")
			}
		})
	}
}
