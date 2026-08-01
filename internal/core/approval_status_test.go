package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestApprovalStalenessFresh(t *testing.T) {
	root := approvedStatusRoot(t)
	status, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || !status.Current || status.Approval == nil || status.Reason != "" {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestApprovalStalenessEnvelopePayloadBinding(t *testing.T) {
	root := approvedStatusRoot(t)
	records, diagnostics, err := record.Replay(
		filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
	)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("history = %#v, %v", diagnostics, err)
	}
	item := records[len(records)-1]
	var approval ApprovalRecord
	if err := json.Unmarshal(item.Payload, &approval); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*record.Record, *ApprovalRecord){
		"change": func(_ *record.Record, approval *ApprovalRecord) { approval.Change = "other-change" },
		"revision": func(_ *record.Record, approval *ApprovalRecord) {
			approval.RevisionBefore++
			approval.RevisionAfter++
		},
		"actor":     func(item *record.Record, _ *ApprovalRecord) { item.Actor = "other@example.com" },
		"timestamp": func(item *record.Record, _ *ApprovalRecord) { item.Timestamp = "2020-01-01T00:00:00Z" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changedItem, changedApproval := item, approval
			mutate(&changedItem, &changedApproval)
			if err := validateApprovalEnvelope(changedItem, changedApproval); err == nil {
				t.Fatal("mismatched envelope accepted")
			}
			changedItem.Payload, _ = json.Marshal(changedApproval)
			if _, _, ok := recoverableApproval(
				[]record.Record{changedItem}, approval.Change, approval.RevisionBefore,
			); ok {
				t.Fatal("mismatched recovery record accepted")
			}
		})
	}
}

func TestApprovalStalenessEveryArtifactAndDeletion(t *testing.T) {
	paths := []string{
		"proposal.md",
		"design.md",
		"tasks.md",
		filepath.Join("specs", "sample", "spec.md"),
	}
	for _, relative := range paths {
		t.Run("changed-"+filepath.Base(relative), func(t *testing.T) {
			root := approvedStatusRoot(t)
			target := filepath.Join(root, ".specd", "changes", "safe-change", relative)
			file, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("\nchanged\n"); err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			status, err := CurrentApprovalStatus(root, "safe-change")
			if err != nil || status.Current || status.BlockingArtifact != filepath.ToSlash(relative) {
				t.Fatalf("status = %#v, %v", status, err)
			}
		})
	}
	t.Run("deleted", func(t *testing.T) {
		root := approvedStatusRoot(t)
		target := filepath.Join(root, ".specd", "changes", "safe-change", "design.md")
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		status, err := CurrentApprovalStatus(root, "safe-change")
		if err != nil || status.Current || status.BlockingArtifact != "design.md" {
			t.Fatalf("status = %#v, %v", status, err)
		}
	})
}

func TestApprovalStalenessArtifactSet(t *testing.T) {
	root := approvedStatusRoot(t)
	target := filepath.Join(root, ".specd", "changes", "safe-change", "specs", "extra", "spec.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCheckFile(t, target, []byte(
		"## Purpose\nExtra.\n## ADDED Requirements\n### Requirement: Extra\nThe system MUST work.\n#### Scenario: Works\n- **WHEN** run\n- **THEN** works\n",
	))
	status, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || status.Current || status.BlockingArtifact != "specs/extra/spec.md" {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestApprovalStalenessRegistryPolicyRevision(t *testing.T) {
	t.Run("registry", func(t *testing.T) {
		root := approvedStatusRoot(t)
		status, err := ProjectApprovalStatus(root, "safe-change", "future-v2", DefaultPolicyDigest())
		if err != nil || status.Current || status.Reason != "registry_version_changed" {
			t.Fatalf("status = %#v, %v", status, err)
		}
	})
	t.Run("policy", func(t *testing.T) {
		root := approvedStatusRoot(t)
		fresh, err := CurrentApprovalStatus(root, "safe-change")
		if err != nil {
			t.Fatal(err)
		}
		status, err := ProjectApprovalStatus(root, "safe-change", fresh.Approval.RegistryVersion, "changed-policy")
		if err != nil || status.Current || status.Reason != "policy_digest_changed" {
			t.Fatalf("status = %#v, %v", status, err)
		}
	})
	t.Run("revision", func(t *testing.T) {
		root := approvedStatusRoot(t)
		path := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
		raw, _ := os.ReadFile(path)
		current, err := state.Decode(raw, "safe-change")
		if err != nil {
			t.Fatal(err)
		}
		current.Revision++
		raw, err = state.Encode(current)
		if err != nil {
			t.Fatal(err)
		}
		writeCheckFile(t, path, raw)
		status, err := CurrentApprovalStatus(root, "safe-change")
		if err != nil || status.Current || status.Reason != "state_revision_changed" {
			t.Fatalf("status = %#v, %v", status, err)
		}
	})
}

func TestApprovalStalenessHistoryUnchanged(t *testing.T) {
	root := approvedStatusRoot(t)
	path := filepath.Join(root, ".specd", "history.jsonl")
	before, _ := os.ReadFile(path)
	if _, err := CurrentApprovalStatus(root, "safe-change"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("status changed immutable history")
	}
}

func TestApprovalRemainsCurrentAcrossCompletionHistory(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	request := completeRequest(3)
	request.AfterHistory = func() error { return errors.New("stop") }
	if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "complete_interrupted") {
		t.Fatalf("interruption = %v", err)
	}
	status, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || !status.Current {
		t.Fatalf("pending completion approval = %#v, %v", status, err)
	}
	request.AfterHistory = nil
	if _, err := CompleteTask(root, "safe-change", request); err != nil {
		t.Fatal(err)
	}
	status, err = CurrentApprovalStatus(root, "safe-change")
	if err != nil || !status.Current {
		t.Fatalf("committed completion approval = %#v, %v", status, err)
	}
}

// Friction is evidence, never authority: an appended observation must leave the
// approval that authorized the observed work exactly as current as it was, or
// the first friction record would silently disarm every later one.
func TestApprovalRemainsCurrentAcrossFrictionHistory(t *testing.T) {
	root := frictionRoot(t)
	before, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || !before.Current {
		t.Fatalf("blocked-task approval = %#v, %v", before, err)
	}
	if _, err := RecordFriction(root, "safe-change", frictionRequest()); err != nil {
		t.Fatal(err)
	}
	after, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || !after.Current || after.Reason != "" ||
		after.Approval == nil || after.Approval.AggregateHash != before.Approval.AggregateHash {
		t.Fatalf("friction staled the approval it observed: %#v, %v", after, err)
	}
	// The invariant that matters to D14: the task is still blocked, so a second
	// independent observation is still recordable rather than hypothetical.
	second := frictionRequest()
	second.Domain = "delivery"
	second.Consequence = "release identity cannot be recorded"
	if _, err := RecordFriction(root, "safe-change", second); err != nil {
		t.Fatalf("second friction record = %v", err)
	}
	if items := historyKinds(t, root, record.KindFriction); len(items) != 2 {
		t.Fatalf("expected two friction records, got %d", len(items))
	}
}

func approvedStatusRoot(t *testing.T) string {
	t.Helper()
	root := checkRoot(t, true)
	_, err := Approve(root, "safe-change", ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com", Reason: "reviewed", Route: ApprovalRouteHumanTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
