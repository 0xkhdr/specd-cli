package cmd

import (
	"path/filepath"
	"testing"
)

// tempRoot is tempRoot(t) resolved through filepath.EvalSymlinks. Every
// production entry point canonicalizes the root it selects, so a raw
// tempRoot(t) disagrees with the root the harness actually uses wherever the
// temporary directory sits under a symlink: /var/folders on macOS, and any
// host with a symlinked TMPDIR. Comparing against the uncanonicalized path is
// the test's bug, not the harness's.
func tempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp root: %v", err)
	}
	return root
}
