package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkStateReadModifyWrite(b *testing.B) {
	for _, tasks := range []int{10, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("tasks=%d", tasks), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "state.json")
			initial := Initial("benchmark", "created")
			for i := range tasks {
				initial.Tasks[fmt.Sprintf("task-%05d", i)] = json.RawMessage(`"pending"`)
			}
			encoded, err := Encode(initial)
			if err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if _, err := Mutate(path, "benchmark", uint64(i+1), func(*State) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
