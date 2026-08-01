package path

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corelock "github.com/0xkhdr/specd-cli/internal/core/lock"
)

func TestResolveRoot(t *testing.T) {
	root := tempRoot(t)
	link := filepath.Join(tempRoot(t), "project")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	got, err := ResolveRoot(link, "")
	if err != nil || got != want {
		t.Fatalf("ResolveRoot() = %q, %v; want %q", got, err, want)
	}
	for _, inputs := range [][2]string{{"", ""}, {root, root}, {filepath.Join(root, "missing"), ""}} {
		if _, err := ResolveRoot(inputs[0], inputs[1]); err == nil {
			t.Fatalf("ResolveRoot%q accepted", inputs)
		}
	}
}

func TestSegment(t *testing.T) {
	for _, valid := range []string{"cache-ttl", "a", "v2-cache"} {
		if err := ValidateSegment(valid); err != nil {
			t.Errorf("ValidateSegment(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "..", "/abs", "a/b", `a\b`, "a\x00b", "Upper", "a--b", "archive", "state"} {
		err := ValidateSegment(invalid)
		var refusal *failure.Refusal
		if !errors.As(err, &refusal) || refusal.Code == "" || refusal.Next != "choose another name" {
			t.Errorf("ValidateSegment(%q) = %#v", invalid, err)
		}
	}
}

func TestManagedPath(t *testing.T) {
	root := tempRoot(t)
	owner, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := owner.ChangeState("cache-ttl"); got != filepath.Join(root, ".specd", "changes", "cache-ttl", "state.json") {
		t.Fatalf("ChangeState = %q", got)
	}
	if err := os.MkdirAll(owner.Changes(), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := tempRoot(t)
	if err := os.Symlink(outside, filepath.Join(owner.Changes(), "escape")); err != nil {
		t.Fatal(err)
	}
	if err := owner.CheckWriteTarget(filepath.Join(owner.Changes(), "escape", "state.json")); !failure.IsCode(err, "path_symlink") {
		t.Fatalf("symlink err = %v", err)
	}
	if err := owner.CheckWriteTarget(filepath.Join(root, "outside")); !failure.IsCode(err, "path_escape") {
		t.Fatalf("escape err = %v", err)
	}
	if err := os.Mkdir(filepath.Join(owner.Changes(), "cache-ttl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := owner.EnsureUniqueChange("cache-ttl"); !failure.IsCode(err, "duplicate_change") {
		t.Fatalf("active duplicate err = %v", err)
	}
	if err := os.Remove(filepath.Join(owner.Changes(), "cache-ttl")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(owner.Archive(), "2026-07-30-cache-ttl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := owner.EnsureUniqueChange("cache-ttl"); !failure.IsCode(err, "duplicate_change") {
		t.Fatalf("archive duplicate err = %v", err)
	}
}

func TestManagedPathChangeLockRequiresExistingChange(t *testing.T) {
	root := tempRoot(t)
	owner, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ChangeLock("cache-ttl"); !failure.IsCode(err, "not_initialized") {
		t.Fatalf("uninitialized err = %v", err)
	}
	if err := os.MkdirAll(owner.Changes(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ChangeLock("cache-ttl"); !failure.IsCode(err, "change_not_found") {
		t.Fatalf("absent change err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(owner.Changes(), "cache-ttl")); !os.IsNotExist(err) {
		t.Fatalf("refused lock created change directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(owner.Changes(), "not-a-dir"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ChangeLock("not-a-dir"); !failure.IsCode(err, "change_not_found") {
		t.Fatalf("file change err = %v", err)
	}
	if err := os.Mkdir(filepath.Join(owner.Changes(), "cache-ttl"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The lock sits beside the change folder, never inside it: archive renames
	// that folder while holding this lock.
	want := filepath.Join(owner.Changes(), "cache-ttl.lock")
	if got, err := owner.ChangeLock("cache-ttl"); err != nil || got != want {
		t.Fatalf("ChangeLock = %q, %v; want %q", got, err, want)
	}
}

func TestManagedPathLockSerializes(t *testing.T) {
	lockPath := filepath.Join(tempRoot(t), "change.lock")
	var wait sync.WaitGroup
	var value int
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := corelock.With(lockPath, func() error {
				current := value
				value = current + 1
				return nil
			}); err != nil {
				t.Errorf("With: %v", err)
			}
		}()
	}
	wait.Wait()
	if value != 20 {
		t.Fatalf("serialized value = %d, want 20", value)
	}
}
