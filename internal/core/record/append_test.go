package record

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestAppendConcurrentDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	record := created(t, "cache-ttl", 0)
	var wait sync.WaitGroup
	wait.Add(2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			defer wait.Done()
			errors <- Append(path, FamilyHistory, record)
		}()
	}
	wait.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful appends = %d, want 1", successes)
	}
	records, diagnostics, err := Replay(path, FamilyHistory)
	if err != nil || len(diagnostics) != 0 || len(records) != 1 {
		t.Fatalf("replay = %d/%d/%v", len(records), len(diagnostics), err)
	}
}

func TestAppendRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.jsonl")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, FamilyHistory, created(t, "a", 0)); err == nil {
		t.Fatal("symlink append accepted")
	}
	if raw, _ := os.ReadFile(outside); string(raw) != "safe" {
		t.Fatalf("outside file changed: %q", raw)
	}
}

func TestAppendRefusesTornTailUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	before := []byte(`{"schema_version":`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, FamilyHistory, created(t, "cache-ttl", 0)); err == nil {
		t.Fatal("append accepted torn tail")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed append changed existing bytes")
	}
}

// TestAppendRevisionChainNamesBothCauses covers the case a deleted change
// directory produces: history is intact and append still refuses, because the
// surviving records already carry the name. Sending that caller to repair the
// file would send them to repair something that is not broken, so the refusal
// must offer restoring the change as well as choosing another name.
func TestAppendRevisionChainNamesBothCauses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := Append(path, FamilyHistory, created(t, "cache-ttl", 0)); err != nil {
		t.Fatal(err)
	}
	// The same name again starts from revision 0 against a chain at 1, exactly
	// as `specd new` does after the change directory was removed.
	recreated, newErr := New(Record{
		Family: FamilyHistory, Kind: KindCreated, Change: "cache-ttl",
		ExpectedRevision: Revision(0), ResultingRevision: Revision(1),
		Timestamp: "2026-07-31T09:00:00Z", Actor: "operator:test",
		Payload: json.RawMessage(`{"status":"planning"}`),
	})
	if newErr != nil {
		t.Fatal(newErr)
	}
	err := Append(path, FamilyHistory, recreated)
	var refused *failure.Refusal
	if !errors.As(err, &refused) || refused.Code != "record-revision-chain" {
		t.Fatalf("append error = %v, want a record-revision-chain refusal", err)
	}
	for _, want := range []string{"restore the change", "choose a name"} {
		if !strings.Contains(refused.Next, want) {
			t.Errorf("next action %q does not offer %q", refused.Next, want)
		}
	}
}
