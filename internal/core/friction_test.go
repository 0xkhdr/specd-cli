package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
)

// frictionRoot is an approved change whose only task the readiness owner
// reports as blocked, which is the one state friction may be recorded against.
func frictionRoot(t *testing.T) string {
	t.Helper()
	root := attemptRoot(t)
	if _, err := TransitionTaskActivity(root, "safe-change", TaskTransitionRequest{
		TaskID: "T1", To: TaskBlocked, ExpectedRevision: 2,
		Authority: trustedTaskTransitionAuthority("agent:builder"),
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func frictionRequest() FrictionRequest {
	return FrictionRequest{
		TaskID: "T1", Operation: "attempt", Domain: "orchestration",
		Consequence: "the task cannot run without a worktree lease",
		Actor:       "agent:builder", ExpectedRevision: 3,
	}
}

func frictionStateBytes(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".specd", "changes", "safe-change", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// appendFrictionRecord writes one friction record straight to history so a
// second independent change can be projected without building a whole change.
func appendFrictionRecord(t *testing.T, root, change, task, domain string, at time.Time) record.Record {
	t.Helper()
	payload, err := record.NewFrictionPayload(Friction{
		Change: change, TaskID: task, Operation: "attempt", Domain: domain,
		Blocker: "task_blocked", Consequence: "blocked by a missing deferred domain",
		Actor: "agent:builder", ObservedRevision: 3,
		ContractHash: strings.Repeat("a", 64), StateHash: strings.Repeat("b", 64),
		EvidenceSet: strings.Repeat("c", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindFriction, Change: change,
		Timestamp: at.UTC().Format(time.RFC3339Nano), Actor: payload.Actor, Payload: encoded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Append(filepath.Join(root, ".specd", "history.jsonl"),
		record.FamilyHistory, item); err != nil {
		t.Fatal(err)
	}
	return item
}

func frictionEligibility(t *testing.T, root, domain string) FrictionEligibility {
	t.Helper()
	rows, err := ProjectFrictionEligibility(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Domain == domain {
			return row
		}
	}
	return FrictionEligibility{Domain: domain}
}

func frictionRefusal(t *testing.T, err error, code string) *failure.Refusal {
	t.Helper()
	return syncRefusal(t, err, code)
}

func TestFrictionRecordsOneRealBlocker(t *testing.T) {
	root := frictionRoot(t)
	before := frictionStateBytes(t, root)

	recorded, err := RecordFriction(root, "safe-change", frictionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Change != "safe-change" || recorded.TaskID != "T1" ||
		recorded.Domain != "orchestration" || recorded.Blocker != "task_blocked" ||
		recorded.ObservedRevision != 3 || recorded.Actor != "agent:builder" {
		t.Fatalf("friction = %+v", recorded)
	}
	items := historyKinds(t, root, record.KindFriction)
	if len(items) != 1 {
		t.Fatalf("expected one friction record, got %d", len(items))
	}
	// The record is an observation: it claims no transition, so it can never
	// move lifecycle, approval, authority, or task activity.
	if items[0].ExpectedRevision != nil || items[0].ResultingRevision != nil {
		t.Fatalf("friction record carries a revision transition: %+v", items[0])
	}
	if string(frictionStateBytes(t, root)) != string(before) {
		t.Fatal("friction mutated change state")
	}
}

func TestFrictionRefusesHypotheticalAndMalformedRecords(t *testing.T) {
	blocked := frictionRoot(t)
	ready := attemptRoot(t)

	cases := []struct {
		name    string
		root    string
		change  string
		mutate  func(*FrictionRequest)
		code    string
		nothing bool
	}{
		{name: "unauthorized", root: blocked, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.Actor = "  " }, code: "friction_unauthorized"},
		{name: "no consequence", root: blocked, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.Consequence = "" }, code: "friction_invalid"},
		{name: "unknown domain", root: blocked, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.Domain = "telemetry" }, code: "friction_domain"},
		{name: "unknown task", root: blocked, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.TaskID = "T9" }, code: "friction_unknown"},
		{name: "stale revision", root: blocked, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.ExpectedRevision = 2 }, code: "stale_revision"},
		// A ready task is the hypothetical case: nothing is blocked, so there is
		// no real friction to record.
		{name: "hypothetical", root: ready, change: "safe-change",
			mutate: func(r *FrictionRequest) { r.ExpectedRevision = 2 }, code: "friction_hypothetical"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := frictionRequest()
			item.mutate(&request)
			_, err := RecordFriction(item.root, item.change, request)
			frictionRefusal(t, err, item.code)
			if records := historyKinds(t, item.root, record.KindFriction); len(records) != 0 {
				t.Fatalf("refused friction appended %d records", len(records))
			}
		})
	}
}

// Observability is the whole hypothetical rule, and every readiness must be
// classified by it: work that can proceed, is proceeding, or is done is stopped
// by nothing, and an unclassified readiness fails closed.
func TestFrictionObservableReadiness(t *testing.T) {
	observable := map[Readiness]bool{
		ReadinessReady: false, ReadinessActive: false, ReadinessTerminal: false,
		ReadinessBlocked: true, ReadinessWaitingDependency: true, ReadinessWaitingApproval: true,
		Readiness("invented"): false,
	}
	for readiness, want := range observable {
		if got := frictionObservable(readiness); got != want {
			t.Fatalf("frictionObservable(%q) = %t, want %t", readiness, got, want)
		}
	}
}

// A friction record that never matched the codec cannot reach history at all,
// so a malformed or hypothetical entry can never become usable by replay.
func TestFrictionRejectsMalformedRecordsAtTheLedger(t *testing.T) {
	root := frictionRoot(t)
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindFriction, Change: "safe-change",
		ExpectedRevision: record.Revision(3), ResultingRevision: record.Revision(4),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Actor: "agent:builder",
		Payload: json.RawMessage(`{"schema_version":1}`),
	})
	if err == nil {
		t.Fatalf("a transitional friction record was constructed: %+v", item)
	}
	if _, err := ProjectFrictionEligibility(root); err != nil {
		t.Fatal(err)
	}
}

func TestFrictionEligibilityNeedsTwoIndependentRecords(t *testing.T) {
	root := frictionRoot(t)
	before := frictionStateBytes(t, root)
	if _, err := RecordFriction(root, "safe-change", frictionRequest()); err != nil {
		t.Fatal(err)
	}
	if row := frictionEligibility(t, root, "orchestration"); row.Records != 1 || row.Eligible {
		t.Fatalf("one record made the domain eligible: %+v", row)
	}

	// A repeated observation of the same change and task is one fact.
	repeat := frictionRequest()
	repeat.Consequence = "still blocked by the same missing lease model"
	if _, err := RecordFriction(root, "safe-change", repeat); err != nil {
		t.Fatal(err)
	}
	if row := frictionEligibility(t, root, "orchestration"); row.Records != 1 || row.Eligible {
		t.Fatalf("a replayed record created eligibility: %+v", row)
	}

	// Each domain is its own bucket, so one root proves every independence rule.
	appendFrictionRecord(t, root, "one-change", "T1", "maintenance", time.Now())
	appendFrictionRecord(t, root, "one-change", "T2", "maintenance", time.Now())
	appendFrictionRecord(t, root, "one-change", "T1", "delivery", time.Now())
	appendFrictionRecord(t, root, "other-change", "T1", "delivery", time.Now())
	appendFrictionRecord(t, root, "one-change", "T1", "references", time.Now())
	appendFrictionRecord(t, root, "other-change", "T2", "references", time.Now())
	if row := frictionEligibility(t, root, "maintenance"); row.Records != 2 || row.Eligible {
		t.Fatalf("two records of one change made the domain eligible: %+v", row)
	}
	if row := frictionEligibility(t, root, "delivery"); row.Records != 2 || row.Eligible {
		t.Fatalf("two records of one task made the domain eligible: %+v", row)
	}
	row := frictionEligibility(t, root, "references")
	if !row.Eligible || row.Records != 2 {
		t.Fatalf("two independent records did not project eligibility: %+v", row)
	}
	// Eligibility is a projection for the root owner: it authorizes nothing.
	if string(frictionStateBytes(t, root)) != string(before) {
		t.Fatal("friction eligibility mutated change state")
	}
	if other := frictionEligibility(t, root, "multi-root"); other.Records != 0 || other.Eligible {
		t.Fatalf("an unobserved domain became eligible: %+v", other)
	}
	current := readTaskTransitionState(t, root)
	activity, err := ProjectTaskActivity(current.Tasks, "T1")
	if err != nil || activity != TaskBlocked {
		t.Fatalf("friction unblocked the task: %q %v", activity, err)
	}
}

func TestFrictionProjectionRefusesFutureRecords(t *testing.T) {
	root := frictionRoot(t)
	appendFrictionRecord(t, root, "safe-change", "T1", "orchestration",
		time.Now().Add(2*time.Hour))
	_, err := ProjectFrictionEligibility(root)
	frictionRefusal(t, err, "friction_future")
}

func TestFrictionConcurrentRecordingStaysAtomic(t *testing.T) {
	root := frictionRoot(t)
	domains := []string{"delivery", "maintenance", "multi-root", "orchestration",
		"references", "security-scanners"}
	var group sync.WaitGroup
	errs := make([]error, len(domains))
	for index, domain := range domains {
		group.Add(1)
		go func(index int, domain string) {
			defer group.Done()
			request := frictionRequest()
			request.Domain = domain
			request.Consequence = "blocked without " + domain
			_, errs[index] = RecordFriction(root, "safe-change", request)
		}(index, domain)
	}
	group.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("%s: %v", domains[index], err)
		}
	}
	// Every append is complete and replayable, and no domain became eligible
	// from one change and one task.
	if records := historyKinds(t, root, record.KindFriction); len(records) != len(domains) {
		t.Fatalf("expected %d friction records, got %d", len(domains), len(records))
	}
	rows, err := ProjectFrictionEligibility(root)
	if err != nil || len(rows) != len(domains) {
		t.Fatalf("eligibility = %+v, %v", rows, err)
	}
	for _, row := range rows {
		if row.Records != 1 || row.Eligible {
			t.Fatalf("concurrent single-task friction created eligibility: %+v", row)
		}
	}
}
