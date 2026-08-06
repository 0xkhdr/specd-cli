package record

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkLedgerReplay(b *testing.B) {
	for _, entries := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "history.jsonl")
			file, err := os.Create(path)
			if err != nil {
				b.Fatal(err)
			}
			for i := range entries {
				record, err := New(Record{
					Family: FamilyHistory, Kind: KindCreated, Change: fmt.Sprintf("change-%d", i),
					ExpectedRevision: Revision(0), ResultingRevision: Revision(1),
					Timestamp: time.Unix(int64(i), 0).UTC().Format(time.RFC3339Nano), Actor: "benchmark",
					Payload: json.RawMessage(`{"benchmark":true}`),
				})
				if err != nil {
					b.Fatal(err)
				}
				line, _ := json.Marshal(record)
				if _, err := file.Write(append(line, '\n')); err != nil {
					b.Fatal(err)
				}
			}
			if err := file.Close(); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, diagnostics, err := Replay(path, FamilyHistory); err != nil || len(diagnostics) != 0 {
					b.Fatalf("replay: diagnostics=%v err=%v", diagnostics, err)
				}
			}
		})
	}
}
