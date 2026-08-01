package lock

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// TestWithRefusesHeldLock covers both waits: a file lock held by another open
// file description, and the in-process mutex held by an outer call. Neither may
// hang; each must refuse with a legal recovery action.
func TestWithRefusesHeldLock(t *testing.T) {
	Deadline = 50 * time.Millisecond
	t.Cleanup(func() { Deadline = 10 * time.Second })

	t.Run("file lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "lock")
		held, err := secureOpen(path)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
		if acquired, err := platformTryLock(held); err != nil || !acquired {
			t.Fatalf("hold lock: acquired=%v err=%v", acquired, err)
		}
		defer platformUnlock(held)

		ran := false
		start := time.Now()
		err = With(path, func() error { ran = true; return nil })
		if !failure.IsCode(err, "lock_busy") || ran {
			t.Fatalf("held lock accepted: err=%v ran=%v", err, ran)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("waited %v past the deadline", elapsed)
		}
	})

	t.Run("in-process mutex", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "lock")
		inner := With(path, func() error {
			return With(path, func() error { return nil })
		})
		if !failure.IsCode(inner, "lock_busy") {
			t.Fatalf("reentrant lock accepted: %v", inner)
		}
	})
}

func TestWithRefusesSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := With(path, func() error { return nil }); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if raw, _ := os.ReadFile(outside); string(raw) != "safe" {
		t.Fatalf("outside file changed: %q", raw)
	}
}
