package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

const packetDesign = "## Boundaries\nsample/Requirement: Stable updates\n## Interfaces\nOne transaction.\n" +
	"## Invariants\nOne writer.\n## Failure behavior\nNo partial write.\n" +
	"## Integration\nExisting owner.\n## Alternatives\nNo distributed lock.\n## Owner\ninternal/sample\n"

// approvedRoot is the ready fixture: authored artifacts, current human
// approval, one attempt recorded against that approval, and one current
// passing observation. Every earlier fixture in this package stops short of
// approval, so the approvable branch needs its own root.
func approvedRoot(t *testing.T, design string) string {
	t.Helper()
	root := emptyRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := filepath.Join(owner.Changes(), reportChange)
	writeFile(t, filepath.Join(change, "proposal.md"),
		"## Problem\nUpdates race.\n## Outcome\nUpdates serialize.\n## Scope\nLocal updates.\n"+
			"## Non-goals\nDistributed locks.\n## Affected capabilities\nsample\n")
	writeFile(t, filepath.Join(change, "design.md"), design)
	writeFile(t, filepath.Join(change, "tasks.md"),
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| T1 | builder | internal/sample.go | | sample/Requirement: Stable updates | "+
			"`go test ./internal/sample` | Sample passes |\n")
	writeFile(t, filepath.Join(change, "specs", "sample", "spec.md"),
		"## Purpose\nSample updates.\n## ADDED Requirements\n### Requirement: Stable updates\n"+
			"The store MUST serialize updates.\n#### Scenario: Concurrent\n- **WHEN** updates race\n"+
			"- **THEN** both commit\n")

	if _, err := core.Approve(root, reportChange, core.ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
		Reason: "reviewed exact planning bytes", Route: core.ApprovalRouteHumanTerminal,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := core.LoadReadinessSnapshot(root, reportChange)
	if err != nil {
		t.Fatal(err)
	}
	approval := snapshot.Approval()
	if approval.Approval == nil || !approval.Current {
		t.Fatalf("approval = %#v", approval)
	}

	task := canonicalTask(t, owner)
	attempt, err := record.NewAttemptPayload(record.AttemptPayload{
		Change: reportChange, TaskID: task.ID, BaselineHEAD: reportHead,
		RevisionBefore: 2, RevisionAfter: 3, ContractHash: core.TaskContractHash(task),
		ApprovalHash: approval.Approval.AggregateHash, StateGuardHash: reportGuard,
		DeclaredFiles: task.Files, Actor: "agent@example.com", Assurance: core.AttemptAssurance,
		ActivityFrom: "pending", ActivityTo: "in_progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	item := appendHistory(t, owner, record.KindAttempt, 2, payload,
		"agent@example.com", reportClock.Add(time.Minute))
	writeState(t, owner, func(current *state.State) {
		current.Revision = 3
		current.LastTransition = item.ID
		current.Tasks = map[string]json.RawMessage{task.ID: json.RawMessage(`"completed"`)}
		current.Extensions = map[string]json.RawMessage{
			"attempts": json.RawMessage(`{"` + task.ID + `":` + string(payload) + `}`),
		}
	})
	subject := reportSubject(t, task, attempt)
	subject.StateRevision = 3
	observation, err := evidence.NewTestRun(subject, verifyexec.Result{
		StartedAt: reportClock.Format(time.RFC3339Nano),
		EndedAt:   reportClock.Add(time.Second).Format(time.RFC3339Nano),
		Passed:    true, NonVacuous: true,
		StdoutDigest: reportDigest, StderrDigest: reportDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.Append(owner.Evidence(), "agent@example.com", observation); err != nil {
		t.Fatal(err)
	}
	return root
}

func packetOf(t *testing.T, root string) ReviewPacket {
	t.Helper()
	packet, err := ProjectReviewPacket(loadTruth(t, root))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func blockerCodes(packet ReviewPacket) []string {
	codes := make([]string, 0, len(packet.Blockers))
	for _, blocker := range packet.Blockers {
		codes = append(codes, blocker.Code)
	}
	return codes
}

func hasBlocker(packet ReviewPacket, code string) bool {
	for _, blocker := range packet.Blockers {
		if blocker.Code == code {
			if strings.TrimSpace(blocker.Action) == "" {
				return false
			}
			return true
		}
	}
	return false
}

// treeDigest fingerprints every managed byte, so "the packet writes nothing"
// is checked rather than assumed.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum.Write([]byte(filepath.ToSlash(relative) + "\x00"))
		sum.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func TestReviewPacketReadyChangeIsApprovable(t *testing.T) {
	packet := packetOf(t, approvedRoot(t, packetDesign))
	if !packet.Approvable || len(packet.Blockers) != 0 {
		t.Fatalf("ready packet blocked by %#v", packet.Blockers)
	}
	if packet.Change != reportChange || packet.Empty ||
		packet.PolicyDigest != core.DefaultPolicy().Digest() ||
		packet.Inconsistency != nil {
		t.Fatalf("packet = %#v", packet)
	}
	// Approved intent, declared scope, and proof travel with the packet.
	if !packet.Review.ApprovalCurrent || packet.Review.ApprovalHash == "" ||
		len(packet.Review.Artifacts) == 0 || len(packet.Review.Capabilities) != 1 ||
		len(packet.Review.Tasks) != 1 || !packet.Review.Proof.Tasks[0].Current {
		t.Fatalf("review = %#v", packet.Review)
	}
	// Design contracts.
	if len(packet.Design) != 2 || packet.Design[0].Name != "Invariants" ||
		!packet.Design[0].Present || packet.Design[0].Excerpt != "One writer." ||
		packet.Design[1].Name != "Failure behavior" || !packet.Design[1].Present ||
		packet.Design[1].Excerpt != "No partial write." {
		t.Fatalf("design = %#v", packet.Design)
	}
	// Diff identity comes from the recorded attempt, never from a fresh diff.
	if packet.Diff.HEAD != reportHead || packet.Diff.Ambiguous ||
		len(packet.Diff.Tasks) != 1 || packet.Diff.Tasks[0].TaskID != "T1" ||
		len(packet.Diff.Tasks[0].Files) != 1 || packet.Diff.Tasks[0].Files[0] != "internal/sample.go" ||
		packet.Diff.Tasks[0].ApprovalHash != packet.Review.ApprovalHash ||
		len(packet.Diff.Unattempted) != 0 {
		t.Fatalf("diff = %#v", packet.Diff)
	}
	// Expected reconciliation effect, by hash and never by document body.
	if !packet.Effect.Applicable || packet.Effect.NoOp || len(packet.Effect.Diagnostics) != 0 ||
		len(packet.Effect.Capabilities) != 1 {
		t.Fatalf("effect = %#v", packet.Effect)
	}
	created := packet.Effect.Capabilities[0]
	if created.Capability != "sample" || !created.Created || created.NoOp ||
		created.BeforeHash != "" || created.AfterHash == "" ||
		created.AcceptedPath != ".specd/specs/sample/spec.md" {
		t.Fatalf("capability effect = %#v", created)
	}
}

func TestReviewPacketPartialTruthCannotBeApprovable(t *testing.T) {
	// authoredRoot is authored and observed but never approved.
	packet := packetOf(t, authoredRoot(t))
	if packet.Approvable || !hasBlocker(packet, "review_approval_stale") {
		t.Fatalf("unapproved packet = %v", blockerCodes(packet))
	}
	// Everything else the reviewer needs is still projected, so a partial
	// packet describes the gap instead of refusing.
	if len(packet.Design) != 2 || !packet.Design[0].Present ||
		packet.Diff.HEAD != reportHead || !packet.Effect.Applicable {
		t.Fatalf("partial packet = %#v", packet)
	}
}

func TestReviewPacketStaleProofAndDisagreementAreVisible(t *testing.T) {
	root := approvedRoot(t, packetDesign)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// One appended record whose state write has not landed: recoverable, so the
	// packet exposes the disagreement and stops presenting an approvable state.
	appendHistory(t, owner, record.KindTaskTransition, 3, json.RawMessage(`{"note":"pending"}`),
		"agent@example.com", reportClock.Add(2*time.Minute))
	packet := packetOf(t, root)
	if packet.Inconsistency == nil || packet.Inconsistency.Code != "history_ahead_of_state" ||
		packet.Approvable || !hasBlocker(packet, "review_disagreement") ||
		!hasBlocker(packet, "review_proof_stale") || !hasBlocker(packet, "review_proof_missing") ||
		len(packet.Review.Proof.Tasks[0].StaleEvidence) != 1 {
		t.Fatalf("packet = %#v, %v", packet.Inconsistency, blockerCodes(packet))
	}

	// Irreconcilable state and history still fail closed with one action.
	writeState(t, owner, func(current *state.State) { current.Revision = 9 })
	_, err = ProjectReviewPacket(loadTruth(t, root))
	reportRefusal(t, err, "report_disagreement")
}

func TestReviewPacketExtraEvidenceIsVisible(t *testing.T) {
	root := approvedRoot(t, packetDesign)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := evidence.NewTestRun(evidence.Subject{
		Change: reportChange, TaskID: "T9", AttemptID: "attempt-t9", HEAD: reportHead,
		TaskHash: reportGuard, CommandHash: reportDigest, ApprovalHash: reportApprove,
		StateRevision: 3,
	}, verifyexec.Result{
		StartedAt: reportClock.Format(time.RFC3339Nano),
		EndedAt:   reportClock.Add(time.Second).Format(time.RFC3339Nano),
		Passed:    true, NonVacuous: true,
		StdoutDigest: reportDigest, StderrDigest: reportDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.Append(owner.Evidence(), "agent@example.com", orphan); err != nil {
		t.Fatal(err)
	}
	packet := packetOf(t, root)
	if len(packet.Review.Proof.ExtraEvidence) != 1 || packet.Approvable ||
		!hasBlocker(packet, "review_evidence_extra") {
		t.Fatalf("extra evidence = %#v, %v", packet.Review.Proof.ExtraEvidence, blockerCodes(packet))
	}
}

func TestReviewPacketEmptyChangeIsExplicit(t *testing.T) {
	packet := packetOf(t, emptyRoot(t))
	if !packet.Empty || packet.Approvable || len(packet.Diff.Tasks) != 0 ||
		len(packet.Effect.Capabilities) != 0 || !hasBlocker(packet, "review_empty") ||
		!hasBlocker(packet, "review_design_incomplete") {
		t.Fatalf("empty packet = %#v, %v", packet, blockerCodes(packet))
	}
	for _, section := range packet.Design {
		if section.Present || section.Excerpt != "" {
			t.Fatalf("unauthored design reported: %#v", section)
		}
	}
}

func TestReviewPacketAmbiguousDiffIdentityIsExposed(t *testing.T) {
	root := approvedRoot(t, packetDesign)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(owner.Changes(), reportChange, "tasks.md"),
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| T1 | builder | internal/sample.go | | sample/Requirement: Stable updates | "+
			"`go test ./internal/sample` | Sample passes |\n"+
			"| T2 | builder | internal/other.go | T1 | sample/Requirement: Stable updates | "+
			"`go test ./internal/other` | Other passes |\n")
	attempt, err := record.NewAttemptPayload(record.AttemptPayload{
		Change: reportChange, TaskID: "T2", BaselineHEAD: strings.Repeat("a", 40),
		RevisionBefore: 3, RevisionAfter: 4, ContractHash: reportGuard,
		ApprovalHash: reportApprove, StateGuardHash: reportGuard,
		DeclaredFiles: []string{"internal/other.go"}, Actor: "agent@example.com",
		Assurance: core.AttemptAssurance, ActivityFrom: "pending", ActivityTo: "in_progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	item := appendHistory(t, owner, record.KindAttempt, 3, payload,
		"agent@example.com", reportClock.Add(3*time.Minute))
	writeState(t, owner, func(current *state.State) {
		current.Revision = 4
		current.LastTransition = item.ID
	})
	packet := packetOf(t, root)
	if !packet.Diff.Ambiguous || packet.Diff.HEAD != "" || len(packet.Diff.Tasks) != 2 ||
		packet.Approvable || !hasBlocker(packet, "review_diff_ambiguous") {
		t.Fatalf("diff = %#v, %v", packet.Diff, blockerCodes(packet))
	}
}

func TestReviewPacketBoundsSecretAndOversizedDesign(t *testing.T) {
	secret := "api_key=super-secret-value\n" + strings.Repeat("invariant line\n", 4096)
	packet := packetOf(t, approvedRoot(t,
		"## Boundaries\nsample/Requirement: Stable updates\n## Interfaces\nOne transaction.\n"+
			"## Invariants\n"+secret+"## Failure behavior\nNo partial write.\n"+
			"## Integration\nExisting owner.\n## Alternatives\nNo lock.\n## Owner\ninternal/sample\n"))
	invariants := packet.Design[0]
	if !invariants.Present || !invariants.Truncated ||
		len(invariants.Excerpt) > agentjson.ExcerptLimit {
		t.Fatalf("oversized design was not bounded: %d bytes, truncated=%v",
			len(invariants.Excerpt), invariants.Truncated)
	}
	if strings.Contains(invariants.Excerpt, "super-secret-value") ||
		!strings.Contains(invariants.Excerpt, "[REDACTED]") {
		t.Fatalf("secret-like design reached the packet: %q", invariants.Excerpt)
	}
}

func TestReviewPacketReplayIsIdenticalAndWritesNothing(t *testing.T) {
	root := approvedRoot(t, packetDesign)
	before := treeDigest(t, root)
	first := packetOf(t, root)
	second := packetOf(t, root)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("packet replay differs:\n%#v\n%#v", first, second)
	}
	if after := treeDigest(t, root); after != before {
		t.Fatal("building a review packet changed local truth")
	}
}
