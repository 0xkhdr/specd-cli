package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestApprovalLifecycle(t *testing.T) {
	states := []Lifecycle{
		LifecyclePlanning, LifecycleApproved, LifecycleExecuting,
		LifecycleReconciling, LifecycleArchived,
	}
	for i, from := range states {
		for j, to := range states {
			err := ValidateApprovalTransition(from, to)
			want := j == i+1
			if want != (err == nil) {
				t.Fatalf("%s to %s error = %v, want legal %v", from, to, err, want)
			}
		}
	}
	for _, transition := range [][2]Lifecycle{
		{"future", LifecycleApproved},
		{LifecyclePlanning, "future"},
		{LifecycleArchived, LifecyclePlanning},
	} {
		if err := ValidateApprovalTransition(transition[0], transition[1]); !failure.IsCode(err, "lifecycle_transition") {
			t.Fatalf("%s to %s error = %v", transition[0], transition[1], err)
		}
	}
}

func TestApprovalIdentityStableAndBindsPathBytes(t *testing.T) {
	root := t.TempDir()
	writeApprovalFile(t, root, "proposal.md", "same")
	writeApprovalFile(t, root, "specs/cache/spec.md", "requirement")
	first, err := ComputeApprovalIdentity(root, []string{"specs/cache/spec.md", "proposal.md"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeApprovalIdentity(root, []string{"proposal.md", "specs/cache/spec.md"})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("identity drift = %#v %#v %v", first, second, err)
	}
	if first.Artifacts[0].Path != "proposal.md" || first.Artifacts[1].Path != "specs/cache/spec.md" {
		t.Fatalf("paths not canonical: %#v", first.Artifacts)
	}

	writeApprovalFile(t, root, "proposal.md", "changed")
	changed, _ := ComputeApprovalIdentity(root, []string{"proposal.md", "specs/cache/spec.md"})
	if changed.AggregateHash == first.AggregateHash {
		t.Fatal("byte change retained aggregate hash")
	}
	writeApprovalFile(t, root, "proposal.md", "same")
	if err := os.Rename(filepath.Join(root, "proposal.md"), filepath.Join(root, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	renamed, _ := ComputeApprovalIdentity(root, []string{"renamed.md", "specs/cache/spec.md"})
	if renamed.AggregateHash == first.AggregateHash {
		t.Fatal("path change retained aggregate hash")
	}
}

func TestApprovalIdentityRejectsUnsafeMissingAndAlias(t *testing.T) {
	root := t.TempDir()
	writeApprovalFile(t, root, "proposal.md", "safe")
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, paths := range [][]string{
		{"../outside.md"}, {"/absolute.md"}, {"proposal.md", "./proposal.md"},
		{"proposal.md", "proposal.md"}, {"missing.md"}, {"linked.md"},
	} {
		if _, err := ComputeApprovalIdentity(root, paths); !failure.IsCode(err, "approval_artifact") {
			t.Fatalf("paths %#v error = %v", paths, err)
		}
	}
	if err := os.Link(filepath.Join(root, "proposal.md"), filepath.Join(root, "alias.md")); err == nil {
		if _, err := ComputeApprovalIdentity(root, []string{"proposal.md", "alias.md"}); !failure.IsCode(err, "approval_artifact") {
			t.Fatalf("hardlink alias error = %v", err)
		}
	}
}

func TestApprovalIdentityRecordValidation(t *testing.T) {
	root := t.TempDir()
	writeApprovalFile(t, root, "proposal.md", "safe")
	identity, err := ComputeApprovalIdentity(root, []string{"proposal.md"})
	if err != nil {
		t.Fatal(err)
	}
	record := ApprovalRecord{
		SchemaVersion: ApprovalSchemaVersion, ID: "approval-1", Change: "safe-change",
		Gate: "planning_to_approved", LifecycleFrom: LifecyclePlanning, LifecycleTo: LifecycleApproved,
		Approver: "human@example.com", ActorClass: "human",
		Artifacts: identity.Artifacts, AggregateHash: identity.AggregateHash,
		RegistryVersion: "core-v1", PolicyDigest: strings.Repeat("a", 64),
		RevisionBefore: 1, RevisionAfter: 2, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Reason: "reviewed exact plan", Assurance: "advisory",
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ApprovalRecord){
		"agent":      func(r *ApprovalRecord) { r.ActorClass = "agent" },
		"gate":       func(r *ApprovalRecord) { r.Gate = "arbitrary" },
		"transition": func(r *ApprovalRecord) { r.LifecycleTo = LifecycleArchived },
		"assurance":  func(r *ApprovalRecord) { r.Assurance = "sandboxed" },
		"change":     func(r *ApprovalRecord) { r.Change = "../escape" },
		"policy":     func(r *ApprovalRecord) { r.PolicyDigest = "not-a-digest" },
		"registry":   func(r *ApprovalRecord) { r.RegistryVersion = "v1" },
		"record id":  func(r *ApprovalRecord) { r.ID = "bad id" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := record
			mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatalf("invalid record accepted: %#v", invalid)
			}
		})
	}
}

func TestApprovalIdentityRejectsVolumePath(t *testing.T) {
	root := t.TempDir()
	if _, err := ComputeApprovalIdentity(root, []string{"C:/artifact.md"}); !failure.IsCode(err, "approval_artifact") {
		t.Fatalf("volume path error = %v", err)
	}
}

func writeApprovalFile(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
