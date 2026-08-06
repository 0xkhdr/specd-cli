package context

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkContextAssembly(b *testing.B) {
	for _, count := range []int{10, 100, 1_000} {
		b.Run(fmt.Sprintf("files=%d", count), func(b *testing.B) {
			root := b.TempDir()
			inputs := make([]SourceRef, count)
			for i := range count {
				name := fmt.Sprintf("input-%05d.txt", i)
				if err := os.WriteFile(filepath.Join(root, name), []byte("benchmark context\n"), 0o600); err != nil {
					b.Fatal(err)
				}
				inputs[i] = SourceRef{Path: name}
			}

			b.ReportAllocs()
			for range b.N {
				if got, err := ResolveContextLanes(root, inputs, nil); err != nil || len(got) != count {
					b.Fatalf("assembled %d files: %v", len(got), err)
				}
			}
		})
	}
}
