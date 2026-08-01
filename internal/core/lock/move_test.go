package lock

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHeldLockDoesNotBlockMovingItsFolder pins the platform fact archive
// depends on. The change lock lives inside the change folder, and archive
// renames that folder into the archive while still holding it. On Unix an open
// file never blocks a rename; on Windows it does unless the handle shares
// delete access, which is why secureOpen asks for FILE_SHARE_DELETE. If this
// fails, the lock has to move out of the folder it guards.
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

	if err := With(filepath.Join(folder, ".lock"), func() error {
		return os.Rename(folder, target)
	}); err != nil {
		t.Fatalf("move the locked folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "state.json")); err != nil {
		t.Fatalf("moved folder is missing its contents: %v", err)
	}
}
