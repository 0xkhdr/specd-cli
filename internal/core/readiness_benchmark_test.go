package core

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func BenchmarkReadinessProjection(b *testing.B) {
	for _, count := range []int{10, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("tasks=%d", count), func(b *testing.B) {
			tasks := make([]plan.Task, count)
			persisted := make(map[string]json.RawMessage, count)
			for i := range count {
				id := fmt.Sprintf("task-%05d", i)
				tasks[i] = plan.Task{ID: id, Valid: true}
				persisted[id] = json.RawMessage(`"pending"`)
			}
			input := plan.Tasks{Tasks: tasks}
			approval := ApprovalStatus{Current: true, Approval: &ApprovalRecord{}}

			b.ReportAllocs()
			for range b.N {
				if got := ProjectReadiness(input, persisted, "approved", approval); len(got.Tasks()) != count {
					b.Fatalf("projected %d tasks", len(got.Tasks()))
				}
			}
		})
	}
}
