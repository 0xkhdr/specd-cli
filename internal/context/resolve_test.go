package context

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestContextLanesRequiredExistingAndProspective(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "docs/input.md", "required\n")
	writeResolveFile(t, root, "internal/existing.go", "package existing\n")
	location := plan.Location{Path: "tasks.md", Line: 5, Column: 1}

	items, err := ResolveContextLanes(root,
		[]SourceRef{{Path: "docs/input.md", Location: location}},
		[]SourceRef{
			{Path: "internal/existing.go", Location: location},
			{Path: "internal/new.go", Location: location},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Lane != LaneRequiredInput || items[0].Digest == "" ||
		string(items[0].Content) != "required\n" || items[0].Location != location {
		t.Fatalf("required item = %#v", items[0])
	}
	if items[1].Lane != LaneOptionalExistingOutput || items[1].Digest == "" ||
		string(items[1].Content) != "package existing\n" {
		t.Fatalf("existing output = %#v", items[1])
	}
	if items[2].Lane != LaneProspectiveOutput || items[2].Digest != "" ||
		items[2].Content != nil || items[2].Path != "internal/new.go" {
		t.Fatalf("prospective output = %#v", items[2])
	}
	if RequiredInputSchemaMismatch == "" {
		t.Fatal("stage-2 required-input schema mismatch is undocumented")
	}
}

func TestResolveBoundedSelectorsDeterministically(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "pkg/z.go", "z")
	writeResolveFile(t, root, "pkg/a.go", "a")
	writeResolveFile(t, root, "pkg/sub/m.go", "m")

	items, err := ResolveContextLanes(root,
		[]SourceRef{{Path: "pkg/*.go"}, {Path: "pkg/**/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, item := range items {
		paths = append(paths, item.Path)
		if item.Lane != LaneRequiredInput || item.Digest == "" || item.Selector == "" {
			t.Fatalf("selector item = %#v", item)
		}
	}
	want := []string{"pkg/a.go", "pkg/z.go", "pkg/a.go", "pkg/sub/m.go", "pkg/z.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestResolveRefusesUnsafeAndMissingWithoutPartialItems(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "safe.md", "safe")
	outside := tempRoot(t)
	writeResolveFile(t, outside, "secret.md", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	tests := []struct {
		name    string
		inputs  []SourceRef
		outputs []SourceRef
	}{
		{name: "missing required", inputs: []SourceRef{{Path: "safe.md"}, {Path: "missing.md"}}},
		{name: "traversal", inputs: []SourceRef{{Path: "../secret.md"}}},
		{name: "absolute", inputs: []SourceRef{{Path: filepath.Join(outside, "secret.md")}}},
		{name: "symlink", inputs: []SourceRef{{Path: "escape.md"}}},
		{name: "unbounded selector", inputs: []SourceRef{{Path: "*.md"}}},
		{name: "bare directory", inputs: []SourceRef{{Path: "pkg"}}},
		{name: "output glob", outputs: []SourceRef{{Path: "pkg/*.go"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := ResolveContextLanes(root, test.inputs, test.outputs)
			if err == nil || items != nil {
				t.Fatalf("items = %#v, error = %v; want nil refusal", items, err)
			}
			refusal, ok := err.(*ResolveError)
			if !ok || refusal.Code == "" || refusal.Owner != "author" || refusal.Next == "" {
				t.Fatalf("non-actionable refusal: %#v", err)
			}
		})
	}
}

func TestResolveRefusesDigestRace(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "required.md", "before")
	resolver := &resolver{
		root: root,
		afterRead: func(path string) {
			if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	items, err := resolver.resolve([]SourceRef{{Path: "required.md"}}, nil)
	if err == nil || items != nil || !strings.Contains(err.Error(), "changed during assembly") {
		t.Fatalf("items = %#v, error = %v; want stale refusal", items, err)
	}
}

func TestResolveRefusesOversizedExactSources(t *testing.T) {
	for _, lane := range []string{"required", "output"} {
		t.Run(lane, func(t *testing.T) {
			root := tempRoot(t)
			target := filepath.Join(root, "large.bin")
			if err := os.WriteFile(target, make([]byte, maxExactBytes+1), 0o600); err != nil {
				t.Fatal(err)
			}
			var inputs, outputs []SourceRef
			if lane == "required" {
				inputs = []SourceRef{{Path: "large.bin"}}
			} else {
				outputs = []SourceRef{{Path: "large.bin"}}
			}
			items, err := ResolveContextLanes(root, inputs, outputs)
			if err == nil || items != nil || !strings.Contains(err.Error(), "exceeds") {
				t.Fatalf("oversized exact source = %#v, %v", items, err)
			}
		})
	}
}

func TestResolveRefusesSameContentReplacement(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "required.md", "same")
	resolver := &resolver{
		root: root,
		afterRead: func(path string) {
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	items, err := resolver.resolve([]SourceRef{{Path: "required.md"}}, nil)
	if err == nil || items != nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("replacement accepted: %#v, %v", items, err)
	}
}

func TestResolveRefusesSelectorMembershipRace(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "pkg/a.go", "a")
	added := false
	resolver := &resolver{
		root: root,
		afterRead: func(string) {
			if added {
				return
			}
			added = true
			writeResolveFile(t, root, "pkg/b.go", "b")
		},
	}
	items, err := resolver.resolve([]SourceRef{{Path: "pkg/*.go"}}, nil)
	if err == nil || items != nil || !strings.Contains(err.Error(), "membership changed") {
		t.Fatalf("selector drift accepted: %#v, %v", items, err)
	}
}

func TestResolveRefusesSymlinkSelectorMatch(t *testing.T) {
	root := tempRoot(t)
	writeResolveFile(t, root, "pkg/a.go", "a")
	outside := tempRoot(t)
	writeResolveFile(t, outside, "secret.go", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "pkg", "escape.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	items, err := ResolveContextLanes(root, []SourceRef{{Path: "pkg/*.go"}}, nil)
	if err == nil || items != nil {
		t.Fatalf("selector admitted symlink: %#v, %v", items, err)
	}
}

func writeResolveFile(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
