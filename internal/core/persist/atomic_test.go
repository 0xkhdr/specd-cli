package persist

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicReplace(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := AtomicReplace(path, []byte("new"), nil); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	for _, step := range []string{"before-create", "after-write", "after-sync", "before-rename"} {
		t.Run(step, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			stop := errors.New("stop")
			err := AtomicReplace(path, []byte("new"), func(got string) error {
				if got == step {
					return stop
				}
				return nil
			})
			if !errors.Is(err, stop) {
				t.Fatalf("got %v", err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != "old" {
				t.Fatalf("promoted candidate: %q", got)
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp-") {
					t.Fatalf("temporary file left behind: %s", entry.Name())
				}
			}
		})
	}
}
