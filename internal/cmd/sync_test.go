package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestSyncResolvesIdentityFromTrustedSourcesOnly(t *testing.T) {
	root := t.TempDir()
	// A non-file reader stands in for a terminal; each case gets its own
	// confirmed prompt so it fails on identity, never on confirmation.
	options := func() SyncOptions {
		return SyncOptions{
			Approver: "claimed@example.com", Reason: "reviewed",
			Input: bytes.NewBufferString("y\n"), Output: &bytes.Buffer{},
		}
	}

	t.Run("no trusted identity", func(t *testing.T) {
		_, err := syncWithSources(root, "safe-change", options(), "", "")
		if !failure.IsCode(err, "approval_identity") {
			t.Fatalf("missing identity = %v", err)
		}
	})

	t.Run("claim disagrees with trusted identity", func(t *testing.T) {
		_, err := syncWithSources(root, "safe-change", options(), "human@example.com", "")
		if !failure.IsCode(err, "approval_identity") {
			t.Fatalf("mismatched claim = %v", err)
		}
	})

	t.Run("trusted sources disagree", func(t *testing.T) {
		_, err := syncWithSources(root, "safe-change", options(), "a@example.com", "b@example.com")
		if !failure.IsCode(err, "approval_identity") {
			t.Fatalf("disagreeing sources = %v", err)
		}
	})
}

func TestSyncIsNeverHandedToAnAgent(t *testing.T) {
	operation, found := core.OperationByID("sync")
	if !found || operation.AgentVisible || operation.Actor != core.ActorHuman {
		t.Fatalf("sync = %+v, want a human-only operation", operation)
	}
	_, err := Dispatch(context.Background(), Request{
		Args: []string{"sync", "safe-change"}, Root: t.TempDir(),
		Actor: "agent:builder", Route: RouteAgent,
	})
	if ExitCode(err) != 2 || !failure.IsCode(err, "human_operation_required") {
		t.Fatalf("agent route = %v (exit %d)", err, ExitCode(err))
	}
	var refusal *failure.Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Next, "human terminal") {
		t.Fatalf("refusal must hand off to a human: %v", err)
	}
}

func TestSyncEnvelopeNamesArchiveNext(t *testing.T) {
	envelope, err := Envelope(Outcome{Operation: "sync", Root: "/project", Value: core.SyncResult{
		SchemaVersion: 1, Change: "safe-change", Approver: "human@example.com",
		PlanHash: strings.Repeat("a", 64), EvidenceSet: strings.Repeat("b", 64),
		Capabilities:   []core.SyncCapability{{Capability: "sample", Path: "specs/sample/spec.md"}},
		HistoryID:      strings.Repeat("c", 64),
		ArchiveTarget:  "archive/2026-07-30-safe-change",
		RevisionBefore: 4, RevisionAfter: 5,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Next.Kind != "operation" || envelope.Next.Operation != "archive" {
		t.Fatalf("next = %+v", envelope.Next)
	}
	if !envelope.OK || envelope.Exit.Code != 0 {
		t.Fatalf("envelope = %+v", envelope)
	}
}
