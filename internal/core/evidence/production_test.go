package evidence

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

func TestProductionEvidenceClassesStayDistinct(t *testing.T) {
	subject := productionSubject(RequiredCheck{Class: ClassBuild, CheckID: "compile"})
	build, err := NewProduction(subject, passingRun())
	if err != nil || !build.Applicable(subject) {
		t.Fatalf("build = %#v, %v", build, err)
	}
	// The same passing record satisfies nothing else: not another class, not
	// another check id, not another task, HEAD, revision, or policy.
	for name, other := range map[string]ProductionSubject{
		"other class":    productionSubject(RequiredCheck{Class: ClassLint, CheckID: "compile"}),
		"other check":    productionSubject(RequiredCheck{Class: ClassBuild, CheckID: "go-vet"}),
		"other task":     withSubject(subject, func(s *ProductionSubject) { s.TaskID = "T2" }),
		"other head":     withSubject(subject, func(s *ProductionSubject) { s.HEAD = strings.Repeat("c", 40) }),
		"other revision": withSubject(subject, func(s *ProductionSubject) { s.StateRevision = 9 }),
		"other policy":   withSubject(subject, func(s *ProductionSubject) { s.PolicyDigest = strings.Repeat("e", 64) }),
		"other approval": withSubject(subject, func(s *ProductionSubject) { s.ApprovalHash = strings.Repeat("f", 64) }),
	} {
		if build.Matches(other) || build.Applicable(other) {
			t.Fatalf("build evidence satisfied %s", name)
		}
	}
	// The executed command is recorded but never widens or narrows applicability.
	rerun := subject
	rerun.CommandHash = strings.Repeat("9", 64)
	if !build.Applicable(rerun) {
		t.Fatal("command identity changed applicability")
	}
	// test-run keeps the stage-5 payload; the production payload refuses it.
	testRun := subject
	testRun.Check = RequiredCheck{Class: ClassTestRun, CheckID: "task-verify"}
	if _, err := NewProduction(testRun, passingRun()); err == nil {
		t.Fatal("production payload accepted the test-run class")
	}
}

func TestProductionEvidenceRefusesUnknownStaleAndInterrupted(t *testing.T) {
	subject := productionSubject(RequiredCheck{Class: ClassLint, CheckID: "go-vet"})
	for name, mutate := range map[string]func(*Production){
		"unknown class":     func(v *Production) { v.Class = "scan" },
		"malformed check":   func(v *Production) { v.CheckID = "Go Vet" },
		"missing check":     func(v *Production) { v.CheckID = "" },
		"unknown policy":    func(v *Production) { v.PolicyDigest = "not-a-digest" },
		"future schema":     func(v *Production) { v.SchemaVersion = SchemaVersion + 1 },
		"malformed head":    func(v *Production) { v.HEAD = "abc" },
		"missing command":   func(v *Production) { v.CommandHash = "" },
		"reviewer on lint":  func(v *Production) { v.Reviewer = "human@example.com" },
		"result disagrees":  func(v *Production) { v.ExitCode = 1 },
		"reversed clock":    func(v *Production) { v.StartedAt, v.EndedAt = v.EndedAt, v.StartedAt },
		"zero revision":     func(v *Production) { v.StateRevision = 0 },
		"non-utc timestamp": func(v *Production) { v.EndedAt = "2026-07-30T10:00:00+02:00" },
	} {
		value, err := NewProduction(subject, passingRun())
		if err != nil {
			t.Fatal(err)
		}
		mutate(&value)
		if value.Validate() == nil {
			t.Fatalf("%s accepted: %#v", name, value)
		}
		if value.Applicable(subject) {
			t.Fatalf("%s was applicable", name)
		}
	}
	// A future record never counts, even when it is otherwise exact.
	future, err := NewProduction(subject, passingRun())
	if err != nil {
		t.Fatal(err)
	}
	ahead := time.Now().UTC().Add(time.Hour)
	future.StartedAt = ahead.Format(time.RFC3339Nano)
	future.EndedAt = ahead.Add(time.Second).Format(time.RFC3339Nano)
	if future.Applicable(subject) {
		t.Fatal("future record was applicable")
	}
	// Timeout and interruption are recorded as bounded failures, never passes.
	for _, run := range []verifyexec.Result{
		{
			StartedAt: passingRun().StartedAt, EndedAt: passingRun().EndedAt,
			ExitCode: verifyexec.TimeoutExitCode, TimedOut: true,
			StdoutDigest: strings.Repeat("a", 64), StderrDigest: strings.Repeat("b", 64),
		},
		{
			StartedAt: passingRun().StartedAt, EndedAt: passingRun().EndedAt,
			ExitCode: verifyexec.InterruptedExitCode, Interrupted: true,
			StdoutDigest: strings.Repeat("a", 64), StderrDigest: strings.Repeat("b", 64),
		},
	} {
		value, err := NewProduction(subject, run)
		if err != nil {
			t.Fatalf("bounded failure was not recorded: %v", err)
		}
		if value.Passed || value.Applicable(subject) {
			t.Fatalf("interrupted run passed: %#v", value)
		}
	}
}

func TestProductionEvidenceReviewNeedsSeparateReviewer(t *testing.T) {
	subject := productionSubject(RequiredCheck{Class: ClassReview, CheckID: "change-review"})
	review, err := NewReview(subject, "human@example.com", true, time.Now())
	if err != nil || !review.Applicable(subject) {
		t.Fatalf("review = %#v, %v", review, err)
	}
	if review.CommandHash != "" {
		t.Fatal("review recorded a command")
	}
	// Review never substitutes for a runnable class.
	build := subject
	build.Check = RequiredCheck{Class: ClassBuild, CheckID: "compile"}
	if review.Matches(build) {
		t.Fatal("review satisfied a build requirement")
	}
	if _, err := NewReview(subject, "  ", true, time.Now()); err == nil {
		t.Fatal("review without a reviewer accepted")
	}
	failedReview, err := NewReview(subject, "human@example.com", false, time.Now())
	if err != nil || failedReview.Passed || failedReview.Applicable(subject) {
		t.Fatalf("failed review = %#v, %v", failedReview, err)
	}
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	if _, err := AppendProduction(path, "human@example.com", review); err == nil {
		t.Fatal("an actor recorded its own review")
	}
	if _, err := AppendProduction(path, "agent:builder", review); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestProductionEvidenceProjectsMissingAndDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	required := []RequiredCheck{
		{Class: ClassBuild, CheckID: "compile"},
		{Class: ClassLint, CheckID: "go-vet"},
	}
	subject := productionSubject(required[0])
	items := replayProduction(t, path)
	missing, drift, err := MissingProduction(items, required, subject.Subject, subject.PolicyDigest)
	if err != nil || drift != "" || len(missing) != 2 {
		t.Fatalf("empty ledger = %#v, %q, %v", missing, drift, err)
	}
	appendProduction(t, path, subject, passingRun())
	items = replayProduction(t, path)
	missing, _, err = MissingProduction(items, required, subject.Subject, subject.PolicyDigest)
	if err != nil || len(missing) != 1 || missing[0] != required[1] {
		t.Fatalf("after build proof = %#v, %v", missing, err)
	}
	// Proof recorded under another policy is drift, not proof.
	lint := productionSubject(required[1])
	lint.PolicyDigest = strings.Repeat("7", 64)
	appendProduction(t, path, lint, passingRun())
	items = replayProduction(t, path)
	missing, drift, err = MissingProduction(items, required, subject.Subject, subject.PolicyDigest)
	if err != nil || len(missing) != 1 || drift != lint.PolicyDigest {
		t.Fatalf("drift = %#v, %q, %v", missing, drift, err)
	}
	// test-run proof stays owned by the core completion path.
	if _, _, err := MissingProduction(items,
		[]RequiredCheck{{Class: ClassTestRun, CheckID: "task-verify"}},
		subject.Subject, subject.PolicyDigest); err == nil {
		t.Fatal("test-run was projected as production proof")
	}
	// An unknown or malformed record fails closed instead of being skipped.
	if err := record.Append(path, record.FamilyEvidence, unknownRecord(t)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MissingProduction(replayProduction(t, path), required,
		subject.Subject, subject.PolicyDigest); err == nil {
		t.Fatal("unknown class accepted")
	}
}

func unknownRecord(t *testing.T) record.Record {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"class": "scan", "check": "secrets"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := record.New(record.Record{
		Family: record.FamilyEvidence, Kind: record.KindObserved, Change: "safe-change",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Actor: "agent:builder", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func appendProduction(t *testing.T, path string, subject ProductionSubject, run verifyexec.Result) {
	t.Helper()
	value, err := NewProduction(subject, run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AppendProduction(path, "agent:builder", value); err != nil {
		t.Fatal(err)
	}
}

func replayProduction(t *testing.T, path string) []record.Record {
	t.Helper()
	items, diagnostics, err := record.Replay(path, record.FamilyEvidence)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("replay = %v, %#v", err, diagnostics)
	}
	return items
}

func withSubject(subject ProductionSubject, mutate func(*ProductionSubject)) ProductionSubject {
	mutate(&subject)
	return subject
}

func productionSubject(check RequiredCheck) ProductionSubject {
	return ProductionSubject{
		Subject: Subject{
			Change: "safe-change", TaskID: "T1", AttemptID: "A1",
			HEAD: strings.Repeat("d", 40), TaskHash: strings.Repeat("1", 64),
			CommandHash: strings.Repeat("2", 64), ApprovalHash: strings.Repeat("3", 64),
			StateRevision: 4,
		},
		Check: check, PolicyDigest: strings.Repeat("5", 64),
	}
}

func passingRun() verifyexec.Result {
	now := time.Now().UTC().Add(-time.Second)
	return verifyexec.Result{
		StartedAt: now.Format(time.RFC3339Nano),
		EndedAt:   now.Add(time.Millisecond).Format(time.RFC3339Nano),
		ExitCode:  0, Passed: true, NonVacuous: true,
		StdoutDigest: strings.Repeat("a", 64), StderrDigest: strings.Repeat("b", 64),
	}
}

func TestProductionReviewBindingsAreExactAndReviewOnly(t *testing.T) {
	subject := productionSubject(RequiredCheck{Class: ClassReview, CheckID: "change-review"})
	subject.EvidenceSet, subject.PacketHash = strings.Repeat("8", 64), strings.Repeat("9", 64)
	value, err := NewReview(subject, "reviewer@example.com", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value.Findings = "declared scope is wider than the task needs"
	if err := value.Validate(); err != nil || !value.Applicable(subject) {
		t.Fatalf("bound review = %#v, %v", value, err)
	}
	// The two review-only bindings are exact: drift in either is not this
	// review, exactly as HEAD, approval, and policy drift are not.
	for name, mutate := range map[string]func(*ProductionSubject){
		"evidence-set drift": func(s *ProductionSubject) { s.EvidenceSet = strings.Repeat("a", 64) },
		"packet drift":       func(s *ProductionSubject) { s.PacketHash = strings.Repeat("b", 64) },
	} {
		if value.Matches(withSubject(subject, mutate)) {
			t.Fatalf("review survived %s", name)
		}
	}
	// A runnable observation never carries them, even when the subject does.
	build := subject
	build.Check = RequiredCheck{Class: ClassBuild, CheckID: "compile"}
	runnable, err := NewProduction(build, passingRun())
	if err != nil || runnable.EvidenceSet != "" || runnable.PacketHash != "" ||
		!runnable.Applicable(build) {
		t.Fatalf("runnable = %#v, %v", runnable, err)
	}
	for name, mutate := range map[string]func(*Production){
		"review bindings on build": func(v *Production) { v.PacketHash = strings.Repeat("9", 64) },
		"findings on build":        func(v *Production) { v.Findings = "looks fine" },
	} {
		drifted := runnable
		mutate(&drifted)
		if drifted.Validate() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// Malformed or unbounded review bindings fail closed.
	for name, mutate := range map[string]func(*Production){
		"malformed evidence set": func(v *Production) { v.EvidenceSet = "not-a-digest" },
		"malformed packet hash":  func(v *Production) { v.PacketHash = "0x1" },
		"unbounded findings":     func(v *Production) { v.Findings = strings.Repeat("f", maximumFindings+1) },
	} {
		drifted := value
		mutate(&drifted)
		if drifted.Validate() == nil || drifted.Applicable(subject) {
			t.Fatalf("%s accepted", name)
		}
	}
	// Findings survive the round trip through the ledger payload.
	decoded, err := DecodeProduction(mustJSON(t, value))
	if err != nil || decoded.Findings != value.Findings ||
		decoded.EvidenceSet != value.EvidenceSet || decoded.PacketHash != value.PacketHash {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
}

func mustJSON(t *testing.T, value Production) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
