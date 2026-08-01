package report

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const (
	reportChange  = "safe-change"
	reportHead    = "1111111111111111111111111111111111111111"
	reportApprove = "2222222222222222222222222222222222222222222222222222222222222222"
	reportGuard   = "3333333333333333333333333333333333333333333333333333333333333333"
	reportDigest  = "4444444444444444444444444444444444444444444444444444444444444444"
)

var reportClock = time.Date(2026, 7, 30, 9, 30, 0, 0, time.UTC)

func reportRefusal(t *testing.T, err error, code string) *failure.Refusal {
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

// emptyRoot is a valid change with no authored artifact beyond the scaffolded
// state: the empty/partial case reports must describe rather than refuse.
func emptyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{owner.Managed(), owner.Specs(), owner.Changes(), owner.Archive(),
		filepath.Join(owner.Changes(), reportChange)} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := record.Ensure(owner.History(), record.FamilyHistory); err != nil {
		t.Fatal(err)
	}
	if err := record.Ensure(owner.Evidence(), record.FamilyEvidence); err != nil {
		t.Fatal(err)
	}
	created := appendHistory(t, owner, record.KindCreated, 0, json.RawMessage(`{}`),
		"author@example.com", reportClock)
	writeState(t, owner, func(current *state.State) {
		*current = state.Initial(reportChange, created.ID)
	})
	return owner.Root()
}

// authoredRoot adds one canonical task with a recorded attempt and one current
// passing test-run observation, which is what proof and review project.
func authoredRoot(t *testing.T) string {
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
	writeFile(t, filepath.Join(change, "design.md"),
		"## Boundaries\nsample/Requirement: Stable updates\n## Interfaces\nOne transaction.\n"+
			"## Invariants\nOne writer.\n## Failure behavior\nNo partial write.\n"+
			"## Integration\nExisting owner.\n## Alternatives\nNo distributed lock.\n## Owner\ninternal/sample\n")
	writeFile(t, filepath.Join(change, "tasks.md"),
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| T1 | builder | internal/sample.go | | sample/Requirement: Stable updates | "+
			"`go test ./internal/sample` | Sample passes |\n")
	writeFile(t, filepath.Join(change, "specs", "sample", "spec.md"),
		"## Purpose\nSample updates.\n## ADDED Requirements\n### Requirement: Stable updates\n"+
			"The store MUST serialize updates.\n#### Scenario: Concurrent\n- **WHEN** updates race\n"+
			"- **THEN** both commit\n")

	task := canonicalTask(t, owner)
	attempt, err := record.NewAttemptPayload(record.AttemptPayload{
		Change: reportChange, TaskID: task.ID, BaselineHEAD: reportHead,
		RevisionBefore: 1, RevisionAfter: 2, ContractHash: core.TaskContractHash(task),
		ApprovalHash: reportApprove, StateGuardHash: reportGuard,
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
	item := appendHistory(t, owner, record.KindAttempt, 1, payload,
		"agent@example.com", reportClock.Add(time.Minute))
	writeState(t, owner, func(current *state.State) {
		current.Revision = 2
		current.LastTransition = item.ID
		current.Tasks = map[string]json.RawMessage{task.ID: json.RawMessage(`"in_progress"`)}
		current.Extensions = map[string]json.RawMessage{
			"attempts": json.RawMessage(`{"` + task.ID + `":` + string(payload) + `}`),
		}
	})
	observation, err := evidence.NewTestRun(reportSubject(t, task, attempt), verifyexec.Result{
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
	return owner.Root()
}

func reportSubject(t *testing.T, task plan.Task, attempt record.AttemptPayload) evidence.Subject {
	t.Helper()
	command, err := verifyexec.ParseCommand(task.Verify)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := verifyexec.CommandDigest(command)
	if err != nil {
		t.Fatal(err)
	}
	return evidence.Subject{
		Change: reportChange, TaskID: task.ID, AttemptID: attempt.ID, HEAD: attempt.BaselineHEAD,
		TaskHash: core.TaskContractHash(task), CommandHash: digest,
		ApprovalHash: attempt.ApprovalHash, StateRevision: 2,
	}
}

func canonicalTask(t *testing.T, owner *corepath.Owner) plan.Task {
	t.Helper()
	authored := plan.ParseChange(owner, reportChange)
	if len(authored.Tasks.Tasks) != 1 {
		t.Fatalf("expected one canonical task, got %d", len(authored.Tasks.Tasks))
	}
	return authored.Tasks.Tasks[0]
}

func appendHistory(t *testing.T, owner *corepath.Owner, kind record.Kind, from uint64, payload json.RawMessage, actor string, at time.Time) record.Record {
	t.Helper()
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: kind, Change: reportChange,
		ExpectedRevision: record.Revision(from), ResultingRevision: record.Revision(from + 1),
		Timestamp: at.UTC().Format(time.RFC3339Nano), Actor: actor, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Append(owner.History(), record.FamilyHistory, item); err != nil {
		t.Fatal(err)
	}
	return item
}

func writeState(t *testing.T, owner *corepath.Owner, mutate func(*state.State)) {
	t.Helper()
	path := filepath.Join(owner.Changes(), reportChange, "state.json")
	current := state.State{}
	if raw, err := os.ReadFile(path); err == nil {
		if current, err = state.Decode(raw, reportChange); err != nil {
			t.Fatal(err)
		}
	}
	mutate(&current)
	encoded, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadTruth(t *testing.T, root string) Truth {
	t.Helper()
	truth, err := Load(root, reportChange, core.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return truth
}

func TestModelEmptyChangeIsExplicit(t *testing.T) {
	truth := loadTruth(t, emptyRoot(t))
	status, err := ProjectStatus(truth)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Empty || len(status.Tasks) != 0 || len(status.Frontier) != 0 ||
		status.Lifecycle != "planning" || status.Revision != 1 ||
		status.Assurance != core.AttemptAssurance ||
		status.PolicyDigest != core.DefaultPolicy().Digest() {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Blockers) == 0 || status.Blockers[0].Code != "plan_invalid" ||
		strings.TrimSpace(status.Blockers[0].Action) == "" {
		t.Fatalf("empty change must name one blocker, got %#v", status.Blockers)
	}
	for _, artifact := range status.Artifacts {
		if artifact.Present {
			t.Fatalf("unauthored artifact reported present: %#v", artifact)
		}
	}
	proof, err := ProjectProof(truth)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.Empty || len(proof.Tasks) != 0 || len(proof.ExtraEvidence) != 0 {
		t.Fatalf("proof = %#v", proof)
	}
	review, err := ProjectReview(truth)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Empty || len(review.Tasks) != 0 || review.ApprovalCurrent {
		t.Fatalf("review = %#v", review)
	}
}

func TestModelProjectsCurrentProofAndReview(t *testing.T) {
	truth := loadTruth(t, authoredRoot(t))
	proof, err := ProjectProof(truth)
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Tasks) != 1 {
		t.Fatalf("proof tasks = %#v", proof.Tasks)
	}
	row := proof.Tasks[0]
	if row.TaskID != "T1" || !row.Current || row.Missing || len(row.StaleEvidence) != 0 ||
		row.AttemptID == "" || !row.VerifyParsable ||
		len(row.Requirements) != 1 || row.Requirements[0] != "sample/Requirement: Stable updates" {
		t.Fatalf("task proof = %#v", row)
	}
	if len(proof.Requirements) != 1 || !proof.Requirements[0].Covered {
		t.Fatalf("requirement proof = %#v", proof.Requirements)
	}
	if len(proof.ExtraEvidence) != 0 {
		t.Fatalf("extra evidence = %#v", proof.ExtraEvidence)
	}
	review, err := ProjectReview(truth)
	if err != nil {
		t.Fatal(err)
	}
	if review.Empty || len(review.Tasks) != 1 || review.Tasks[0].ID != "T1" ||
		len(review.Tasks[0].Files) != 1 || review.Tasks[0].Files[0] != "internal/sample.go" ||
		len(review.Capabilities) != 1 || review.Capabilities[0].Capability != "sample" ||
		review.Capabilities[0].Added != 1 || review.Synced != nil || review.Archived != nil {
		t.Fatalf("review = %#v", review)
	}
	if !review.Proof.Tasks[0].Current {
		t.Fatal("review packet must carry current proof")
	}
}

func TestModelReplayIsIdenticalAfterRestart(t *testing.T) {
	root := authoredRoot(t)
	first := loadTruth(t, root)
	second := loadTruth(t, root)
	for _, project := range []struct {
		name  string
		value func(Truth) (any, error)
	}{
		{KindStatus, func(truth Truth) (any, error) { return ProjectStatus(truth) }},
		{KindProof, func(truth Truth) (any, error) { return ProjectProof(truth) }},
		{KindHistory, func(truth Truth) (any, error) { return ProjectHistory(truth) }},
		{KindReview, func(truth Truth) (any, error) { return ProjectReview(truth) }},
	} {
		before, err := project.value(first)
		if err != nil {
			t.Fatal(err)
		}
		after, err := project.value(second)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s replay differs:\n%#v\n%#v", project.name, before, after)
		}
	}
}

func TestModelStateHistoryDisagreementFailsClosed(t *testing.T) {
	root := authoredRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	writeState(t, owner, func(current *state.State) { current.Revision = 9 })
	truth := loadTruth(t, root)
	for _, project := range []func(Truth) error{
		func(truth Truth) error { _, err := ProjectStatus(truth); return err },
		func(truth Truth) error { _, err := ProjectProof(truth); return err },
		func(truth Truth) error { _, err := ProjectHistory(truth); return err },
		func(truth Truth) error { _, err := ProjectReview(truth); return err },
	} {
		reportRefusal(t, project(truth), "report_disagreement")
	}
}

func TestModelPendingHistoryBlocksCurrentProof(t *testing.T) {
	root := authoredRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// One appended record whose state write has not landed: recoverable, so the
	// report exposes it instead of refusing, and proof stops being current.
	appendHistory(t, owner, record.KindTaskTransition, 2, json.RawMessage(`{"note":"pending"}`),
		"agent@example.com", reportClock.Add(2*time.Minute))
	truth := loadTruth(t, root)
	status, err := ProjectStatus(truth)
	if err != nil {
		t.Fatal(err)
	}
	if status.Inconsistency == nil || status.Inconsistency.Code != "history_ahead_of_state" ||
		strings.TrimSpace(status.Inconsistency.Action) == "" {
		t.Fatalf("status inconsistency = %#v", status.Inconsistency)
	}
	proof, err := ProjectProof(truth)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Inconsistency == nil || proof.Tasks[0].Current ||
		len(proof.Tasks[0].StaleEvidence) != 1 {
		t.Fatalf("proof = %#v", proof.Tasks)
	}
}

// reversedTimestampRoot records a later event with an earlier timestamp, which
// is exactly the case where timestamps must not decide semantic order.
func reversedTimestampRoot(t *testing.T) string {
	t.Helper()
	root := emptyRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	item := appendHistory(t, owner, record.KindTaskTransition, 1, json.RawMessage(`{"note":"later"}`),
		"agent@example.com", reportClock.Add(-time.Hour))
	writeState(t, owner, func(current *state.State) {
		current.Revision = 2
		current.LastTransition = item.ID
	})
	return root
}

func TestHistoryOrderFollowsRevisionNotTimestamp(t *testing.T) {
	truth := loadTruth(t, reversedTimestampRoot(t))
	// File order already runs forward by revision; reversing the slice proves
	// order comes from revision and append sequence, never from the clock.
	truth.History[0], truth.History[1] = truth.History[1], truth.History[0]
	model, err := ProjectHistory(truth)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Events) != 2 || model.Empty {
		t.Fatalf("events = %#v", model.Events)
	}
	if model.Events[0].Kind != string(record.KindCreated) || model.Events[0].Revision != 1 ||
		model.Events[1].Kind != string(record.KindTaskTransition) || model.Events[1].Revision != 2 {
		t.Fatalf("reversed timestamps changed order: %#v", model.Events)
	}
	if model.Events[0].Timestamp <= model.Events[1].Timestamp {
		t.Fatal("fixture must keep timestamps running backward")
	}
}

func TestHistoryOrderFailsClosedOnAmbiguityAndFutureRecords(t *testing.T) {
	root := authoredRoot(t)

	duplicate := loadTruth(t, root)
	duplicate.History = append(duplicate.History, duplicate.History[1])
	_, err := ProjectHistory(duplicate)
	reportRefusal(t, err, "report_event_ambiguous")

	future := loadTruth(t, root)
	future.History[1].SchemaVersion = record.SchemaVersion + 1
	_, err = ProjectHistory(future)
	reportRefusal(t, err, "report_event_unknown")

	gap := loadTruth(t, root)
	gap.History = gap.History[1:]
	_, err = ProjectHistory(gap)
	reportRefusal(t, err, "report_event_gap")

	unloaded := Truth{Policy: core.DefaultPolicy()}
	_, err = ProjectHistory(unloaded)
	reportRefusal(t, err, "report_source")
}

// frictionRecord is one appended observation: it carries no revision
// transition, so it must reach the model without touching lifecycle ordering.
func frictionRecord(t *testing.T, owner *corepath.Owner, task string, at time.Time) record.Record {
	t.Helper()
	payload, err := record.NewFrictionPayload(record.FrictionPayload{
		Change: reportChange, TaskID: task, Operation: "attempt",
		Domain: "orchestration", Blocker: "task_blocked",
		Consequence: "the task cannot run without a worktree lease",
		Actor:       "agent@example.com", ObservedRevision: 2,
		ContractHash: reportApprove, StateHash: reportGuard, EvidenceSet: reportDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindFriction, Change: reportChange,
		Timestamp: at.UTC().Format(time.RFC3339Nano), Actor: payload.Actor, Payload: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Append(owner.History(), record.FamilyHistory, item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestHistoryOrderCarriesFrictionOutsideRevisionOrder(t *testing.T) {
	root := authoredRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately stamped before every lifecycle record: an observation must
	// neither reorder the lifecycle nor be ordered by its own clock.
	first := frictionRecord(t, owner, "T1", reportClock.Add(-time.Hour))
	second := frictionRecord(t, owner, "T2", reportClock.Add(time.Hour))

	model, err := ProjectHistory(loadTruth(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Events) != 2 || model.Empty ||
		model.Events[0].Revision != 1 || model.Events[1].Revision != 2 {
		t.Fatalf("friction disturbed lifecycle ordering: %#v", model.Events)
	}
	if len(model.Observations) != 2 ||
		model.Observations[0].ID != first.ID || model.Observations[1].ID != second.ID ||
		model.Observations[0].Sequence != 0 || model.Observations[1].Sequence != 1 ||
		model.Observations[0].Revision != 2 ||
		model.Observations[0].Kind != string(record.KindFriction) {
		t.Fatalf("observations = %#v", model.Observations)
	}
	// Every other report reads the same truth and stays unaffected by it.
	status, err := ProjectStatus(loadTruth(t, root))
	if err != nil || status.Inconsistency != nil || status.Revision != 2 {
		t.Fatalf("status = %#v, %v", status, err)
	}

	// A friction record still fails closed when it is not what it claims.
	malformed := loadTruth(t, root)
	for index := range malformed.History {
		if malformed.History[index].Kind == record.KindFriction {
			malformed.History[index].Payload = json.RawMessage(`{"schema_version":1}`)
		}
	}
	_, err = ProjectHistory(malformed)
	reportRefusal(t, err, "report_event_invalid")
}

func TestModelProductionProofNamesMissingChecks(t *testing.T) {
	truth, err := Load(authoredRoot(t), reportChange, core.ProductionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ProjectProof(truth)
	if err != nil {
		t.Fatal(err)
	}
	if proof.PolicyDigest != core.ProductionPolicy().Digest() ||
		len(proof.Tasks[0].MissingChecks) != len(core.ProductionPolicy().RequiredChecks) {
		t.Fatalf("production proof = %#v", proof.Tasks[0])
	}
}
