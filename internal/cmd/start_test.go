package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
)

func TestStartAfterReadOnlyContextBindsAttemptOnce(t *testing.T) {
	root := statusReadinessRoot(t)
	startGit(t, root)
	statePath := mustStatePath(t, root, "safe-change")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Context(root, "safe-change", "ready", 0)
	if err != nil {
		t.Fatal(err)
	}
	afterContext, _ := os.ReadFile(statePath)
	if string(before) != string(afterContext) {
		t.Fatal("context mutated state")
	}
	result, err := Start(root, "safe-change", "ready", manifest.StateRevision, "agent:builder")
	if err != nil || result.AttemptID == "" ||
		result.RevisionBefore != manifest.StateRevision ||
		result.RevisionAfter != manifest.StateRevision+1 ||
		result.Assurance != core.AttemptAssurance ||
		len(result.DeclaredFiles) != 1 || result.DeclaredFiles[0] != "internal/ready.go" {
		t.Fatalf("start = %#v, %v", result, err)
	}
	afterStart, _ := os.ReadFile(statePath)
	if string(afterContext) == string(afterStart) {
		t.Fatal("start did not persist attempt")
	}
	if _, err := Start(root, "safe-change", "ready", result.RevisionAfter, "agent:builder"); err == nil {
		t.Fatal("repeated start succeeded")
	}
}

func TestStartRefusesUnauthorizedAndStaleWithoutMutation(t *testing.T) {
	root := statusReadinessRoot(t)
	startGit(t, root)
	statePath := mustStatePath(t, root, "safe-change")
	before, _ := os.ReadFile(statePath)
	tests := []struct {
		revision uint64
		actor    string
	}{
		{revision: 1, actor: "agent:builder"},
		{revision: 2, actor: ""},
	}
	for _, test := range tests {
		if _, err := Start(root, "safe-change", "ready", test.revision, test.actor); err == nil {
			t.Fatalf("invalid start succeeded: %#v", test)
		}
		after, _ := os.ReadFile(statePath)
		if string(before) != string(after) {
			t.Fatal("refused start mutated state")
		}
	}
}

func TestStartRefusesDirtyAndNonFrontierWithoutAttempt(t *testing.T) {
	t.Run("dirty", func(t *testing.T) {
		root := statusReadinessRoot(t)
		startGit(t, root)
		if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Start(root, "safe-change", "ready", 2, "agent:builder"); err == nil {
			t.Fatal("dirty start succeeded")
		}
	})
	t.Run("non-frontier", func(t *testing.T) {
		root := statusReadinessRoot(t)
		startGit(t, root)
		if _, err := Start(root, "safe-change", "waiting", 2, "agent:builder"); err == nil {
			t.Fatal("non-frontier start succeeded")
		}
	})
}

func startGit(t *testing.T, root string) {
	t.Helper()
	for _, arguments := range [][]string{
		{"init"}, {"config", "user.email", "human@example.com"},
		{"config", "user.name", "Human"}, {"add", "."},
		{"commit", "-m", "baseline", "--no-gpg-sign"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
}
