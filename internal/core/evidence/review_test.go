package evidence

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
)

func reviewBinding() ReviewBinding {
	subject := productionSubject(RequiredCheck{Class: ClassReview, CheckID: "change-review"})
	subject.EvidenceSet, subject.PacketHash = strings.Repeat("8", 64), strings.Repeat("9", 64)
	return ReviewBinding{
		ProductionSubject: subject,
		Approver:          "human@example.com",
		Implementer:       "agent:builder",
	}
}

func TestProductionReviewRecordsOneSeparateVerdict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	binding := reviewBinding()
	value, item, err := RecordReview(path, "agent:builder", binding,
		"reviewer@example.com", "", true, time.Now())
	if err != nil || !value.Passed || value.Reviewer != "reviewer@example.com" ||
		value.CommandHash != "" || item.ID == "" {
		t.Fatalf("verdict = %#v, %#v, %v", value, item, err)
	}
	verdict, err := CurrentReview(replayProduction(t, path), binding)
	if err != nil || verdict.State != ReviewApproved || verdict.RecordID != item.ID ||
		verdict.Reviewer != "reviewer@example.com" {
		t.Fatalf("current = %#v, %v", verdict, err)
	}
	// The verdict proves review for this subject only: never a runnable class,
	// and never another task, commit, or policy.
	for name, mutate := range map[string]func(*ReviewBinding){
		"code drift":         func(b *ReviewBinding) { b.HEAD = strings.Repeat("c", 40) },
		"artifact drift":     func(b *ReviewBinding) { b.ApprovalHash = strings.Repeat("f", 64) },
		"task drift":         func(b *ReviewBinding) { b.TaskHash = strings.Repeat("e", 64) },
		"policy drift":       func(b *ReviewBinding) { b.PolicyDigest = strings.Repeat("7", 64) },
		"revision drift":     func(b *ReviewBinding) { b.StateRevision = 9 },
		"evidence-set drift": func(b *ReviewBinding) { b.EvidenceSet = strings.Repeat("a", 64) },
		"packet drift":       func(b *ReviewBinding) { b.PacketHash = strings.Repeat("b", 64) },
	} {
		drifted := reviewBinding()
		mutate(&drifted)
		verdict, err := CurrentReview(replayProduction(t, path), drifted)
		if err != nil || verdict.State != ReviewStale {
			t.Fatalf("%s = %#v, %v", name, verdict, err)
		}
	}
	build := binding
	build.Check = RequiredCheck{Class: ClassBuild, CheckID: "compile"}
	if value.Matches(build.ProductionSubject) {
		t.Fatal("review satisfied a runnable check")
	}
	// The verdict itself is not part of the set it binds, so writing it cannot
	// stale it; a later runnable observation does move the set.
	empty, err := EvidenceSetHash(replayProduction(t, path), binding.Change)
	if err != nil {
		t.Fatal(err)
	}
	appendProduction(t, path, productionSubject(RequiredCheck{Class: ClassBuild, CheckID: "compile"}), passingRun())
	moved, err := EvidenceSetHash(replayProduction(t, path), binding.Change)
	if err != nil || moved == empty {
		t.Fatalf("evidence set = %q, %q, %v", empty, moved, err)
	}
	// Evidence on another change never moves this set.
	other, err := EvidenceSetHash(replayProduction(t, path), "other-change")
	if err != nil || other == moved {
		t.Fatalf("other change set = %q, %v", other, err)
	}
	drifted := binding
	drifted.EvidenceSet = moved
	verdict, err = CurrentReview(replayProduction(t, path), drifted)
	if err != nil || verdict.State != ReviewStale {
		t.Fatalf("evidence drift = %#v, %v", verdict, err)
	}
}

func TestProductionReviewFailsClosedOnActorAndRecord(t *testing.T) {
	binding := reviewBinding()
	for name, reviewer := range map[string]string{
		"unknown actor": "  ",
		"approver":      "human@example.com",
		"implementer":   "AGENT:BUILDER",
	} {
		path := filepath.Join(t.TempDir(), "evidence.jsonl")
		if _, _, err := RecordReview(path, "agent:builder", binding, reviewer, "", true, time.Now()); err == nil {
			t.Fatalf("%s recorded a verdict", name)
		}
		if items := replayProduction(t, path); len(items) != 0 {
			t.Fatalf("%s wrote evidence: %#v", name, items)
		}
	}
	// A reject carries findings and blocks; it never counts as proof.
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	if _, _, err := RecordReview(path, "agent:builder", binding,
		"reviewer@example.com", "", false, time.Now()); err == nil {
		t.Fatal("a reject without findings was recorded")
	}
	if _, _, err := RecordReview(path, "agent:builder", binding,
		"reviewer@example.com", "scope is wider than declared", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	verdict, err := CurrentReview(replayProduction(t, path), binding)
	if err != nil || verdict.State != ReviewRejected || verdict.Reason == "" ||
		verdict.Findings != "scope is wider than declared" {
		t.Fatalf("reject = %#v, %v", verdict, err)
	}
	// A reject never satisfies the required review check.
	missing, drift, err := MissingProduction(replayProduction(t, path),
		[]RequiredCheck{binding.Check}, binding.Subject, binding.PolicyDigest)
	if err != nil || drift != "" || len(missing) != 1 || missing[0] != binding.Check {
		t.Fatalf("reject satisfied review proof: %#v, %q, %v", missing, drift, err)
	}
	// A future or malformed record never counts.
	future := filepath.Join(t.TempDir(), "evidence.jsonl")
	if _, _, err := RecordReview(future, "agent:builder", binding, "reviewer@example.com",
		"", true, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if verdict, err := CurrentReview(replayProduction(t, future), binding); err == nil {
		t.Fatalf("future verdict counted: %#v", verdict)
	}
	if err := record.Append(future, record.FamilyEvidence, unknownRecord(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := CurrentReview(replayProduction(t, future), binding); err == nil {
		t.Fatal("unknown evidence class accepted")
	}
	// The class is exact: no other class may be projected as review.
	lint := binding
	lint.Check = RequiredCheck{Class: ClassLint, CheckID: "go-vet"}
	if _, err := CurrentReview(nil, lint); err == nil {
		t.Fatal("a runnable class was projected as review")
	}
}
