package record

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func FuzzRecordLedger(f *testing.F) {
	first, err := New(Record{
		Family: FamilyHistory, Kind: KindCreated, Change: "sample",
		ExpectedRevision: Revision(0), ResultingRevision: Revision(1),
		Timestamp: "2026-08-06T00:00:00Z", Actor: "human@example.com",
		Payload: json.RawMessage(`{"change":"sample"}`),
	})
	if err != nil {
		f.Fatal(err)
	}
	line, err := json.Marshal(first)
	if err != nil {
		f.Fatal(err)
	}
	valid := append(append([]byte(nil), line...), '\n')
	for offset := range valid {
		f.Add(append([]byte(nil), valid[:offset]...))
	}
	for _, seed := range [][]byte{
		valid,
		append(append([]byte(nil), valid...), '{'),
		append(append([]byte(nil), valid...), 0),
		append([]byte(nil), append(line, '\r', '\n')...),
		[]byte(`{"schema_version":2}` + "\n"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		ledger := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.WriteFile(ledger, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		records, diagnostics, err := Replay(ledger, FamilyHistory)
		if err != nil {
			var refusal *failure.Refusal
			if !errors.As(err, &refusal) || refusal.Code == "" || refusal.Next == "" || records != nil || diagnostics != nil {
				t.Fatalf("malformed ledger did not fail closed: records=%#v diagnostics=%#v err=%v", records, diagnostics, err)
			}
			return
		}
		for _, item := range records {
			if err := item.Validate(); err != nil {
				t.Fatalf("replay returned invalid record: %v", err)
			}
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Code != "record-torn-tail" {
				t.Fatalf("unexpected recovery diagnostic: %#v", diagnostic)
			}
		}
	})
}
