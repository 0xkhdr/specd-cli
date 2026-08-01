package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	root := tempRoot(t)
	result, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, ".specd", "keep")
	if err := os.WriteFile(fixture, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(fixture); string(got) != "mine" {
		t.Fatalf("fixture changed: %q", got)
	}
	if result.Root.Path != root {
		t.Fatalf("root = %q", result.Root.Path)
	}
	for _, name := range []string{"specs", "changes", "archive", "history.jsonl", "evidence.jsonl", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(root, ".specd", name)); err != nil {
			t.Fatal(err)
		}
	}
	// Locks are the only managed files Git must not track; planning artifacts,
	// state, history and evidence stay tracked truth.
	ignore, err := os.ReadFile(filepath.Join(root, ".specd", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ignore) != managedIgnore {
		t.Fatalf("managed ignore = %q", ignore)
	}
	for _, pattern := range []string{".root.lock", ".records.lock", "changes/*.lock"} {
		if !strings.Contains(string(ignore), pattern+"\n") {
			t.Fatalf("managed ignore lacks %q: %q", pattern, ignore)
		}
	}
}

func TestInitPreservesAuthoredIgnore(t *testing.T) {
	root := tempRoot(t)
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".specd", ".gitignore")
	if err := os.WriteFile(target, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "mine\n" {
		t.Fatalf("authored ignore changed: %q", got)
	}
}
