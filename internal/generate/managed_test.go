package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// staleRegion builds a well-formed managed region holding old content, so a
// refresh has something to replace without hand-editing a current one.
func staleRegion(body string) string {
	return fmt.Sprintf(markerFormat, 1, hash(body)) + "\n" + body + markerEnd + "\n"
}

func writeGuidance(t *testing.T, root, content string) string {
	t.Helper()
	target := filepath.Join(root, GuidanceFile)
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func readGuidance(t *testing.T, target string) string {
	t.Helper()
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestManagedRefresh(t *testing.T) {
	rendered, err := Render()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("fresh generation is the whole file", func(t *testing.T) {
		root := t.TempDir()
		result, err := Refresh(root)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Changed || result.Hash != rendered.Hash {
			t.Fatalf("result = %#v", result)
		}
		if got := readGuidance(t, result.Path); got != wrap(rendered) {
			t.Fatalf("generated file = %q", got)
		}
		// An atomic replace leaves no partial file behind.
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != GuidanceFile {
			t.Fatalf("refresh left %v behind", entries)
		}
		// The second refresh is a no-op, which is what makes a parity check safe
		// to run against a real project.
		again, err := Refresh(root)
		if err != nil {
			t.Fatal(err)
		}
		if again.Changed {
			t.Fatal("refresh is not idempotent")
		}
	})

	t.Run("user bytes survive and drift is reported once", func(t *testing.T) {
		const prefix = "# House rules\n\nRead these first.\n\n"
		const suffix = "\n## Local notes\n\nKeep the staging URL here.\n"
		root := t.TempDir()
		target := writeGuidance(t, root, prefix+staleRegion("old guidance\n")+suffix)

		drifted, repair, err := Drift(root)
		if err != nil {
			t.Fatal(err)
		}
		if !drifted || repair == "" {
			t.Fatalf("drift = %v, repair = %q", drifted, repair)
		}
		if _, err := Refresh(root); err != nil {
			t.Fatal(err)
		}
		updated := readGuidance(t, target)
		if !strings.HasPrefix(updated, prefix) || !strings.HasSuffix(updated, suffix) {
			t.Fatalf("user bytes changed: %q", updated)
		}
		if updated != prefix+wrap(rendered)+suffix {
			t.Fatalf("refreshed file = %q", updated)
		}
		if strings.Count(updated, markerBegin) != 1 || strings.Count(updated, markerEnd) != 1 {
			t.Fatalf("file holds more than one managed region: %q", updated)
		}
		if drifted, _, err := Drift(root); err != nil || drifted {
			t.Fatalf("drift after refresh = %v (%v)", drifted, err)
		}
	})

	t.Run("appends beside user text that has no region", func(t *testing.T) {
		root := t.TempDir()
		target := writeGuidance(t, root, "# Team notes\n")
		if _, err := Refresh(root); err != nil {
			t.Fatal(err)
		}
		if got := readGuidance(t, target); got != "# Team notes\n\n"+wrap(rendered) {
			t.Fatalf("appended file = %q", got)
		}
	})

	t.Run("unsafe or drifted input refuses before mutation", func(t *testing.T) {
		current := wrap(rendered)
		cases := map[string]struct {
			content string
			code    string
		}{
			"duplicate region":  {current + current, "managed_markers_ambiguous"},
			"inverted markers":  {markerEnd + "\nbody\n" + fmt.Sprintf(markerFormat, 1, hash("body\n")), "managed_markers_ambiguous"},
			"unbalanced marker": {fmt.Sprintf(markerFormat, 1, hash("body\n")) + "\nbody\n", "managed_markers_ambiguous"},
			"unparsable marker": {"<!-- specd:begin schema=one -->\nbody\n" + markerEnd + "\n", "managed_markers_ambiguous"},
			"hash drift":        {strings.Replace(current, "## The loop", "## The loop I edited", 1), "managed_hash_drift"},
		}
		for name, testCase := range cases {
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				target := writeGuidance(t, root, testCase.content)
				_, err := Refresh(root)
				if !failure.IsCode(err, testCase.code) {
					t.Fatalf("refresh = %v, want %s", err, testCase.code)
				}
				if got := readGuidance(t, target); got != testCase.content {
					t.Fatalf("refusal mutated the file: %q", got)
				}
				if err := Clean(root, GuidanceFile); !failure.IsCode(err, testCase.code) {
					t.Fatalf("clean = %v, want %s", err, testCase.code)
				}
				if _, err := os.Lstat(target); err != nil {
					t.Fatalf("refused cleanup removed the file: %v", err)
				}
			})
		}
	})

	t.Run("unsafe destinations refuse", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "user.md")
		if err := os.WriteFile(outside, []byte("user\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, GuidanceFile)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Refresh(root); !failure.IsCode(err, "generation_path_symlink") {
			t.Fatalf("symlink refresh = %v", err)
		}
		if err := Clean(root, GuidanceFile); !failure.IsCode(err, "generation_path_symlink") {
			t.Fatalf("symlink clean = %v", err)
		}
		if got := readGuidance(t, outside); got != "user\n" {
			t.Fatalf("symlink target changed: %q", got)
		}
		for _, name := range []string{"../AGENTS.md", "sub/AGENTS.md", "/etc/AGENTS.md", "NOTES.md"} {
			if _, err := Destination(root, name); !failure.IsCode(err, "generation_target_unknown") {
				t.Fatalf("destination %q = %v", name, err)
			}
		}
	})

	t.Run("cleanup removes only proven managed files", func(t *testing.T) {
		root := t.TempDir()
		target := writeGuidance(t, root, "# Mine\n\n"+wrap(rendered))
		if err := Clean(root, GuidanceFile); !failure.IsCode(err, "cleanup_unproven") {
			t.Fatalf("clean with user text = %v", err)
		}
		if _, err := os.Lstat(target); err != nil {
			t.Fatalf("user file removed: %v", err)
		}
		writeGuidance(t, root, wrap(rendered))
		if err := Clean(root, GuidanceFile); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("managed file survived cleanup: %v", err)
		}
		// A file that is already gone is a valid state, not an error.
		if err := Clean(root, GuidanceFile); err != nil {
			t.Fatal(err)
		}
	})
}
