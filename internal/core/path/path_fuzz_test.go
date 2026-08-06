package path_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

func FuzzChangePathContainment(f *testing.F) {
	for _, seed := range []string{
		"safe-create", "", ".", "..", "../escape", "/abs", `C:\abs`,
		"a/../../b", "con", "nul", "com1", "lpt9", ".specd", "archive",
		"a\x00b", "a\nb", " leading", "trailing ", strings.Repeat("a", 4096),
		"ünïcode", "a/b", `a\b`, "-flag", "--root",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		root := t.TempDir()
		owner, err := corepath.New(root)
		if err != nil {
			t.Fatal(err)
		}
		target, err := owner.Change(name)
		if err != nil {
			var refusal *failure.Refusal
			if !errors.As(err, &refusal) || refusal.Code == "" || refusal.Next == "" {
				t.Fatalf("rejection lacks actionable refusal: %v", err)
			}
			return
		}
		relative, err := filepath.Rel(owner.Changes(), target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			t.Fatalf("accepted name %q escaped changes root: %q", name, target)
		}
		if err := owner.CheckWriteTarget(target); err != nil {
			t.Fatalf("accepted name %q produced unsafe target %q: %v", name, target, err)
		}
	})
}
