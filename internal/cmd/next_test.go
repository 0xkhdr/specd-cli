package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestNextReadinessFullFrontierAndImmutable(t *testing.T) {
	root := statusReadinessRoot(t)
	setNextActivities(t, root, map[string]json.RawMessage{})
	statePath := mustStatePath(t, root, "safe-change")
	historyPath := filepath.Join(root, ".specd", "history.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeHistory, _ := os.ReadFile(historyPath)

	result, err := Next(root, "safe-change", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ready", "active", "failed", "blocked", "done"}
	if !reflect.DeepEqual(result.Frontier, want) || result.Classification != "frontier" ||
		result.Selected != nil {
		t.Fatalf("next = %#v", result)
	}
	afterState, _ := os.ReadFile(statePath)
	afterHistory, _ := os.ReadFile(historyPath)
	if !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(beforeHistory, afterHistory) {
		t.Fatal("next mutated managed state or history")
	}
}

func TestNextReadinessExplicitMembership(t *testing.T) {
	root := statusReadinessRoot(t)
	selected, err := Next(root, "safe-change", "ready")
	if err != nil || selected.Selected == nil || selected.Selected.ID != "ready" ||
		selected.Classification != "selected" {
		t.Fatalf("selected = %#v, %v", selected, err)
	}
	if _, err := Next(root, "safe-change", "waiting"); err == nil ||
		!failure.IsCode(err, "dependency_incomplete") {
		t.Fatalf("waiting error = %v", err)
	} else if refusal, ok := err.(*NextRefusal); !ok || refusal.Owner != "agent" ||
		refusal.Next != "complete the listed dependency tasks" {
		t.Fatalf("waiting refusal = %#v", err)
	}
	if _, err := Next(root, "safe-change", "absent"); err == nil ||
		!failure.IsCode(err, "plan_invalid") {
		t.Fatalf("absent error = %v", err)
	}
}

func TestNextReadinessNoFrontierClasses(t *testing.T) {
	t.Run("all complete", func(t *testing.T) {
		root := statusReadinessRoot(t)
		all := map[string]json.RawMessage{}
		for _, id := range []string{"ready", "active", "waiting", "failed", "blocked", "done"} {
			all[id] = json.RawMessage(`"completed"`)
		}
		setNextActivities(t, root, all)
		result, err := Next(root, "safe-change", "")
		if err != nil || result.Classification != "all_complete" || len(result.Frontier) != 0 {
			t.Fatalf("complete = %#v, %v", result, err)
		}
	})
	t.Run("approval stale", func(t *testing.T) {
		root := statusReadinessRoot(t)
		path := filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md")
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteString("\ndrift\n")
		_ = file.Close()
		result, err := Next(root, "safe-change", "")
		if err != nil || result.Classification != "waiting_approval" ||
			result.Blocker == nil || result.Blocker.Code != "approval_stale" {
			t.Fatalf("stale = %#v, %v", result, err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Init(root); err != nil {
			t.Fatal(err)
		}
		if _, err := New(root, "broken-change", "tester", "sample"); err != nil {
			t.Fatal(err)
		}
		result, err := Next(root, "broken-change", "")
		if err != nil || result.Classification != "plan_invalid" ||
			result.Blocker == nil || result.Blocker.Code != "plan_invalid" {
			t.Fatalf("invalid = %#v, %v", result, err)
		}
	})
}

func setNextActivities(t *testing.T, root string, activities map[string]json.RawMessage) {
	t.Helper()
	path := mustStatePath(t, root, "safe-change")
	raw, _ := os.ReadFile(path)
	current, err := state.Decode(raw, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	current.Tasks = activities
	raw, err = state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	writeStatusFile(t, path, raw)
}
