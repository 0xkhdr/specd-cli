package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

func TestEvidenceAppendAndStrictApplicability(t *testing.T) {
	path := evidencePath(t)
	subject := evidenceSubject()
	pass := executionResult(0, true)
	value, err := NewTestRun(subject, pass)
	if err != nil {
		t.Fatal(err)
	}
	item, err := Append(path, "agent:builder", value)
	if err != nil || item.ID == "" {
		t.Fatalf("append = %#v, %v", item, err)
	}
	all, applicable, err := Project(path, subject)
	if err != nil || len(all) != 1 || applicable == nil || !applicable.Passed {
		t.Fatalf("projection = %#v, %#v, %v", all, applicable, err)
	}

	stale := []Subject{
		with(subject, func(s *Subject) { s.Change = "other" }),
		with(subject, func(s *Subject) { s.TaskID = "T2" }),
		with(subject, func(s *Subject) { s.AttemptID = hash("b") }),
		with(subject, func(s *Subject) { s.HEAD = hash40("b") }),
		with(subject, func(s *Subject) { s.TaskHash = hash("b") }),
		with(subject, func(s *Subject) { s.CommandHash = hash("b") }),
		with(subject, func(s *Subject) { s.ApprovalHash = hash("b") }),
		with(subject, func(s *Subject) { s.StateRevision++ }),
		with(subject, func(s *Subject) { s.StateRevision-- }),
	}
	for _, current := range stale {
		visible, got, err := Project(path, current)
		if err != nil || len(visible) != 1 || got != nil {
			t.Fatalf("stale projection = %#v, %#v, %v", visible, got, err)
		}
	}
	failure, _ := NewTestRun(subject, executionResult(1, false))
	if _, err := Append(path, "agent:builder", failure); err != nil {
		t.Fatal(err)
	}
	visible, got, err := Project(path, subject)
	if err != nil || len(visible) != 2 || got != nil {
		t.Fatalf("latest failure did not supersede pass: %#v, %#v, %v", visible, got, err)
	}
}

func TestEvidenceEveryObservedOutcomeVisibleAndUnusable(t *testing.T) {
	path := evidencePath(t)
	subject := evidenceSubject()
	results := []verifyexec.Result{
		executionResult(1, false),
		func() verifyexec.Result {
			r := executionResult(verifyexec.TimeoutExitCode, false)
			r.TimedOut = true
			return r
		}(),
		func() verifyexec.Result {
			r := executionResult(verifyexec.InterruptedExitCode, false)
			r.Interrupted = true
			return r
		}(),
		func() verifyexec.Result {
			r := executionResult(verifyexec.ZeroMatchExitCode, false)
			r.ZeroMatch = true
			return r
		}(),
	}
	for _, result := range results {
		value, err := NewTestRun(subject, result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Append(path, "agent:builder", value); err != nil {
			t.Fatal(err)
		}
	}
	all, applicable, err := Project(path, subject)
	if err != nil || len(all) != len(results) || applicable != nil {
		t.Fatalf("projection = %#v, %#v, %v", all, applicable, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"stdout_excerpt", "stderr_excerpt", "://", "../"} {
		if bytesContains(raw, forbidden) {
			t.Fatalf("durable evidence leaked %q: %s", forbidden, raw)
		}
	}
}

func TestEvidenceMalformedFutureDuplicateAndAmbiguousFailClosed(t *testing.T) {
	t.Run("future observation", func(t *testing.T) {
		path := evidencePath(t)
		result := executionResult(0, true)
		future := time.Now().UTC().Add(time.Hour)
		result.StartedAt = future.Format(time.RFC3339Nano)
		result.EndedAt = future.Add(time.Second).Format(time.RFC3339Nano)
		value, err := NewTestRun(evidenceSubject(), result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Append(path, "agent:builder", value); err != nil {
			t.Fatal(err)
		}
		visible, applicable, err := Project(path, evidenceSubject())
		if err != nil || len(visible) != 1 || applicable != nil {
			t.Fatalf("future projection = %#v, %#v, %v", visible, applicable, err)
		}
	})
	t.Run("malformed payload", func(t *testing.T) {
		path := evidencePath(t)
		appendOpaque(t, path, json.RawMessage(`{"schema_version":2,"class":"test-run"}`))
		if _, applicable, err := Project(path, evidenceSubject()); err == nil || applicable != nil {
			t.Fatalf("malformed projection = %#v, %v", applicable, err)
		}
	})
	t.Run("duplicate append", func(t *testing.T) {
		path := evidencePath(t)
		value, _ := NewTestRun(evidenceSubject(), executionResult(0, true))
		if _, err := Append(path, "agent:builder", value); err != nil {
			t.Fatal(err)
		}
		if _, err := Append(path, "agent:builder", value); err == nil {
			t.Fatal("duplicate evidence appended")
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		value, _ := NewTestRun(evidenceSubject(), executionResult(0, true))
		raw, _ := json.Marshal(value)
		raw[len(raw)-1] = ','
		raw = append(raw, []byte(`"future":true}`)...)
		if _, err := Decode(raw); err == nil {
			t.Fatal("future payload accepted")
		}
	})
}

func evidencePath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".specd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.jsonl")
	if err := record.Ensure(path, record.FamilyEvidence); err != nil {
		t.Fatal(err)
	}
	return path
}

func evidenceSubject() Subject {
	return Subject{
		Change: "safe-change", TaskID: "T1", AttemptID: hash("a"), HEAD: hash40("a"),
		TaskHash: hash("c"), CommandHash: hash("d"), ApprovalHash: hash("e"), StateRevision: 3,
	}
}

func executionResult(exit int, pass bool) verifyexec.Result {
	return verifyexec.Result{
		CommandDigest: hash("d"), StartedAt: "2026-01-01T00:00:00Z",
		EndedAt: "2026-01-01T00:00:01Z", ExitCode: exit, Passed: pass,
		NonVacuous: pass, StdoutDigest: hash("f"), StderrDigest: hash("0"),
	}
}

func hash(character string) string   { return repeat(character, 64) }
func hash40(character string) string { return repeat(character, 40) }
func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}

func with(subject Subject, mutate func(*Subject)) Subject {
	mutate(&subject)
	return subject
}

func appendOpaque(t *testing.T, path string, payload json.RawMessage) {
	t.Helper()
	item, err := record.New(record.Record{
		Family: record.FamilyEvidence, Kind: record.KindObserved, Change: "safe-change",
		Timestamp: "2026-01-01T00:00:01Z", Actor: "agent", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Append(path, record.FamilyEvidence, item); err != nil {
		t.Fatal(err)
	}
}

func bytesContains(raw []byte, value string) bool {
	for index := 0; index+len(value) <= len(raw); index++ {
		if string(raw[index:index+len(value)]) == value {
			return true
		}
	}
	return false
}
