package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func created(t *testing.T, change string, before uint64) Record {
	t.Helper()
	record, err := New(Record{
		Family: FamilyHistory, Kind: KindCreated, Change: change,
		ExpectedRevision: Revision(before), ResultingRevision: Revision(before + 1),
		Timestamp: "2026-07-30T12:00:00Z", Actor: "operator:test",
		Payload: json.RawMessage(`{"status":"planning"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestIdentityStableCanonicalPayload(t *testing.T) {
	first := created(t, "cache-ttl", 0)
	second := first
	second.ID = ""
	second.Payload = json.RawMessage(`{ "status" : "planning" }`)
	id, err := Identity(second)
	if err != nil {
		t.Fatal(err)
	}
	if id != first.ID {
		t.Fatalf("identity changed: %s != %s", id, first.ID)
	}
	second.Change = "other"
	id, _ = Identity(second)
	if id == first.ID {
		t.Fatal("content change retained identity")
	}
}

func TestApprovalRecord(t *testing.T) {
	approved, err := New(Record{
		Family: FamilyHistory, Kind: KindApproved, Change: "safe-change",
		ExpectedRevision: Revision(1), ResultingRevision: Revision(2),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "human@example.com",
		Payload: json.RawMessage(`{"approval":"bound"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Kind != KindApproved || approved.Validate() != nil {
		t.Fatalf("invalid approval history record: %#v", approved)
	}
	approved.ResultingRevision = Revision(3)
	if err := approved.Validate(); err == nil {
		t.Fatal("revision gap accepted")
	}
}

func TestTaskTransitionRecord(t *testing.T) {
	transition, err := New(Record{
		Family: FamilyHistory, Kind: KindTaskTransition, Change: "safe-change",
		ExpectedRevision: Revision(2), ResultingRevision: Revision(3),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder",
		Payload: json.RawMessage(`{"task":"T1","from":"pending","to":"in_progress"}`),
	})
	if err != nil || transition.Validate() != nil {
		t.Fatalf("task transition record = %#v, %v", transition, err)
	}
	transition.ResultingRevision = Revision(4)
	if err := transition.Validate(); err == nil {
		t.Fatal("task transition revision gap accepted")
	}
}

func TestAttemptRecord(t *testing.T) {
	payload, err := NewAttemptPayload(AttemptPayload{
		Change: "safe-change", TaskID: "T1",
		BaselineHEAD:   strings.Repeat("a", 40),
		RevisionBefore: 2, RevisionAfter: 3,
		ContractHash: strings.Repeat("b", 64), ApprovalHash: strings.Repeat("c", 64),
		StateGuardHash: strings.Repeat("d", 64),
		DeclaredFiles:  []string{"internal/a.go"}, Actor: "agent:builder",
		Assurance: "advisory", ActivityFrom: "pending", ActivityTo: "in_progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	attempt, err := New(Record{
		Family: FamilyHistory, Kind: KindAttempt, Change: "safe-change",
		ExpectedRevision: Revision(2), ResultingRevision: Revision(3),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder",
		Payload: raw,
	})
	if err != nil || attempt.Validate() != nil {
		t.Fatalf("attempt record = %#v, %v", attempt, err)
	}
	attempt.ResultingRevision = Revision(4)
	if err := attempt.Validate(); err == nil {
		t.Fatal("attempt revision gap accepted")
	}
	if _, err := New(Record{
		Family: FamilyHistory, Kind: KindAttempt, Change: "safe-change",
		ExpectedRevision: Revision(2), ResultingRevision: Revision(3),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder",
		Payload: json.RawMessage(`{"attempt":"bound"}`),
	}); err == nil {
		t.Fatal("generic attempt payload accepted")
	}
}

func TestCompletionRecordRequiresExactEvidenceBinding(t *testing.T) {
	payload, err := NewCompletionPayload(CompletionPayload{
		Change: "safe-change", TaskID: "T1",
		AttemptID: strings.Repeat("a", 64), EvidenceID: strings.Repeat("b", 64),
		Actor: "agent:builder", ActivityFrom: "in_progress", ActivityTo: "completed",
		RevisionBefore: 3, RevisionAfter: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	completion, err := New(Record{
		Family: FamilyHistory, Kind: KindCompletion, Change: "safe-change",
		ExpectedRevision: Revision(3), ResultingRevision: Revision(4),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder", Payload: raw,
	})
	if err != nil || completion.Validate() != nil {
		t.Fatalf("completion record = %#v, %v", completion, err)
	}
	for _, malformed := range []json.RawMessage{
		json.RawMessage(`{"task":"T1"}`),
		json.RawMessage(strings.Replace(string(raw), strings.Repeat("b", 64), "not-evidence", 1)),
	} {
		if _, err := New(Record{
			Family: FamilyHistory, Kind: KindCompletion, Change: "safe-change",
			ExpectedRevision: Revision(3), ResultingRevision: Revision(4),
			Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder", Payload: malformed,
		}); err == nil {
			t.Fatal("malformed completion binding accepted")
		}
	}
}

func TestFrictionRecordIsAnObservationWithoutTransition(t *testing.T) {
	payload, err := NewFrictionPayload(FrictionPayload{
		Change: "safe-change", TaskID: "T1", Operation: "attempt",
		Domain: "orchestration", Blocker: "task_blocked",
		Consequence: "the task cannot run without a worktree lease",
		Actor:       "agent:builder", ObservedRevision: 3,
		ContractHash: strings.Repeat("a", 64), StateHash: strings.Repeat("b", 64),
		EvidenceSet: strings.Repeat("c", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	friction, err := New(Record{
		Family: FamilyHistory, Kind: KindFriction, Change: "safe-change",
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder", Payload: raw,
	})
	if err != nil || friction.Validate() != nil {
		t.Fatalf("friction record = %#v, %v", friction, err)
	}
	// A friction record that claims a transition, another actor, another
	// change, or a malformed payload is not a record at all.
	if _, err := New(Record{
		Family: FamilyHistory, Kind: KindFriction, Change: "safe-change",
		ExpectedRevision: Revision(3), ResultingRevision: Revision(4),
		Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder", Payload: raw,
	}); err == nil {
		t.Fatal("friction record with a revision transition accepted")
	}
	for _, mismatch := range []Record{
		{Family: FamilyHistory, Kind: KindFriction, Change: "safe-change",
			Timestamp: "2026-07-30T00:00:00Z", Actor: "human@example.com", Payload: raw},
		{Family: FamilyHistory, Kind: KindFriction, Change: "other-change",
			Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder", Payload: raw},
		{Family: FamilyHistory, Kind: KindFriction, Change: "safe-change",
			Timestamp: "2026-07-30T00:00:00Z", Actor: "agent:builder",
			Payload: json.RawMessage(`{"schema_version":1,"task":"T1"}`)},
	} {
		if _, err := New(mismatch); err == nil {
			t.Fatalf("friction record %+v accepted", mismatch)
		}
	}
}

func TestReplayTailAndStrictCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	first := created(t, "a", 0)
	raw, _ := json.Marshal(first)
	if err := os.WriteFile(path, append(append(raw, '\n'), `{"schema_version":`...), 0o600); err != nil {
		t.Fatal(err)
	}
	records, diagnostics, err := Replay(path, FamilyHistory)
	if err != nil || len(records) != 1 || len(diagnostics) != 1 {
		t.Fatalf("records/diagnostics/error = %d/%d/%v", len(records), len(diagnostics), err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\n{bad}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	records, _, err = Replay(path, FamilyHistory)
	if err == nil || records != nil {
		t.Fatalf("complete corruption trusted: %#v, %v", records, err)
	}
}

func TestReplayDuplicateKeysAndPerChangeChains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	one := created(t, "a", 0)
	two := created(t, "b", 0)
	var lines []string
	for _, record := range []Record{one, two} {
		raw, _ := json.Marshal(record)
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if records, _, err := Replay(path, FamilyHistory); err != nil || len(records) != 2 {
		t.Fatalf("per-change chains rejected: %d, %v", len(records), err)
	}

	duplicateKey := strings.Replace(lines[0], `"actor":"operator:test"`, `"actor":"operator:test","actor":"other"`, 1)
	if err := os.WriteFile(path, []byte(duplicateKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if records, _, err := Replay(path, FamilyHistory); err == nil || records != nil {
		t.Fatal("duplicate JSON key accepted")
	}
}

func TestReplayMissingAndEmptyFileHaveNoRecords(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "history.jsonl")
	records, diagnostics, err := Replay(missing, FamilyHistory)
	if err != nil || records != nil || diagnostics != nil {
		t.Fatalf("missing file = %#v/%#v/%v", records, diagnostics, err)
	}
	if err := os.WriteFile(missing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	records, diagnostics, err = Replay(missing, FamilyHistory)
	if err != nil || len(records) != 0 || len(diagnostics) != 0 {
		t.Fatalf("empty file = %#v/%#v/%v", records, diagnostics, err)
	}
}

func TestReplayRefusesUnknownKindAndFutureSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	raw, _ := json.Marshal(created(t, "a", 0))
	for _, corrupt := range []string{
		strings.Replace(string(raw), `"kind":"created"`, `"kind":"invented"`, 1),
		strings.Replace(string(raw), `"schema_version":1`, `"schema_version":2`, 1),
	} {
		if corrupt == string(raw) {
			t.Fatalf("corruption did not apply: %s", corrupt)
		}
		if err := os.WriteFile(path, []byte(corrupt+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if records, _, err := Replay(path, FamilyHistory); err == nil || records != nil {
			t.Fatalf("untrusted line accepted: %s", corrupt)
		}
	}
}

func TestReplayRefusesNonContiguousRevisionChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	gap, err := New(Record{
		Family: FamilyHistory, Kind: KindApproved, Change: "a",
		ExpectedRevision: Revision(2), ResultingRevision: Revision(3),
		Timestamp: "2026-07-30T12:00:01Z", Actor: "human@example.com",
		Payload: json.RawMessage(`{"approval":"bound"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(created(t, "a", 0))
	second, _ := json.Marshal(gap)
	if err := os.WriteFile(path, []byte(string(first)+"\n"+string(second)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if records, _, err := Replay(path, FamilyHistory); err == nil || records != nil {
		t.Fatalf("non-contiguous chain trusted: %#v, %v", records, err)
	}
}

func TestReplayRefusesSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "history.jsonl")
	record := created(t, "a", 0)
	raw, _ := json.Marshal(record)
	if err := os.WriteFile(outside, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if records, _, err := Replay(path, FamilyHistory); err == nil || records != nil {
		t.Fatal("symlink replay trusted")
	}
}

func TestLedgerLockSerializesEvidenceAndHistory(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.jsonl")
	evidencePath := filepath.Join(dir, "evidence.jsonl")
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = WithLedgerLock(historyPath, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	var entered atomic.Bool
	go func() {
		_ = WithLedgerLock(evidencePath, func() error {
			entered.Store(true)
			return nil
		})
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	if entered.Load() {
		t.Fatal("shared ledger transaction entered while history lock was held")
	}
	close(release)
	<-done
	if !entered.Load() {
		t.Fatal("shared ledger transaction never entered")
	}
}
