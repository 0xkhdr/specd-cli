package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContextFrontierParityAndReadOnly(t *testing.T) {
	root := statusReadinessRoot(t)
	statePath := mustStatePath(t, root, "safe-change")
	before, _ := os.ReadFile(statePath)
	status, err := Status(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Context(root, "safe-change", "ready", 0)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(statePath)
	if !reflect.DeepEqual(manifest.Frontier, status.Frontier) ||
		manifest.StateRevision != status.Revision ||
		manifest.ApprovalHash != status.ApprovalStatus.Approval.AggregateHash {
		t.Fatalf("context/status parity: manifest=%#v status=%#v", manifest, status)
	}
	if string(before) != string(after) {
		t.Fatal("context mutated state")
	}
	raw, err := RenderContextJSON(manifest)
	if err != nil || !strings.Contains(string(raw), `"assurance":"advisory"`) ||
		!strings.Contains(string(raw), `"manifestHash":"`) {
		t.Fatalf("JSON = %s, %v", raw, err)
	}
}

func TestContextFrontierParityRefusesBlockedStaleAndUnknown(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		root := statusReadinessRoot(t)
		if manifest, err := Context(root, "safe-change", "blocked", 0); err == nil || manifest.Version != "" {
			t.Fatalf("blocked context = %#v, %v", manifest, err)
		}
	})
	t.Run("stale", func(t *testing.T) {
		root := statusReadinessRoot(t)
		path := filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md")
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteString("\ndrift\n")
		_ = file.Close()
		if manifest, err := Context(root, "safe-change", "ready", 0); err == nil || manifest.Version != "" {
			t.Fatalf("stale context = %#v, %v", manifest, err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		root := statusReadinessRoot(t)
		if manifest, err := Context(root, "safe-change", "unknown", 0); err == nil || manifest.Version != "" {
			t.Fatalf("unknown context = %#v, %v", manifest, err)
		}
	})
}
