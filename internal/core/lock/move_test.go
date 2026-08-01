package lock

import (
	"os"
	"path/filepath"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

// TestHeldLockDoesNotBlockMovingItsFolder pins the platform fact archive
// depends on: archive renames the change folder into the archive while holding
// that change's lock. Windows refuses to rename a directory that holds an open
// handle — sharing delete access on the handle does not lift that — which is
// why the lock sits beside the folder rather than inside it. A lock that moved
// back inside would pass on Unix and refuse every archive on Windows.
func TestHeldLockDoesNotBlockMovingItsFolder(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "change")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "archived")

	if err := With(corepath.ChangeLockFor(folder), func() error {
		return os.Rename(folder, target)
	}); err != nil {
		t.Fatalf("move the locked folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "state.json")); err != nil {
		t.Fatalf("moved folder is missing its contents: %v", err)
	}
}
