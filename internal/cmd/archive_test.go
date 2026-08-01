package cmd

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
)

func TestArchiveRequiresAnActor(t *testing.T) {
	_, err := Archive(tempRoot(t), "safe-change", ArchiveOptions{})
	if err == nil || !strings.Contains(err.Error(), "next:") {
		t.Fatalf("missing actor = %v", err)
	}
}

func TestArchiveEnvelopeIsTerminalAndLocal(t *testing.T) {
	envelope, err := Envelope(Outcome{Operation: "archive", Root: "/project", Value: core.ArchiveResult{
		SchemaVersion: 1, Change: "safe-change",
		Source: "changes/safe-change", Target: "archive/2026-07-30-safe-change",
		ChangeHash: strings.Repeat("a", 64), Accepted: []string{"sample"},
		EvidenceSet: strings.Repeat("b", 64), Approver: "human@example.com",
		SyncRecord: strings.Repeat("c", 64), TransactionID: strings.Repeat("d", 64),
		HistoryID: strings.Repeat("e", 64), RevisionBefore: 5, RevisionAfter: 6,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Next.Kind != "terminal" || envelope.Next.Operation != "" {
		t.Fatalf("next = %+v", envelope.Next)
	}
	// The end of the local loop claims nothing about delivery.
	rendered := RenderText(envelope)
	for _, forbidden := range []string{"deploy", "push", "pull request", "release"} {
		if strings.Contains(strings.ToLower(rendered), forbidden) {
			t.Fatalf("archive output implies %q: %s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "archive/2026-07-30-safe-change") {
		t.Fatalf("archive output omits the local target: %s", rendered)
	}
}
