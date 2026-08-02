package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/core"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/report"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func mustOwner(t *testing.T, root string) *corepath.Owner {
	t.Helper()
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestApprovalHandoffStatus(t *testing.T) {
	root := tempRoot(t)
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "cache-ttl", "tester", "sample"); err != nil {
		t.Fatal(err)
	}
	result, err := Status(root, "cache-ttl")
	if err != nil {
		t.Fatal(err)
	}
	if result.Approval == nil || !result.Approval.HumanApprovalRequired ||
		result.Approval.Gate != "planning_to_approved" ||
		result.Approval.Assurance != "advisory" ||
		len(result.Approval.Findings) == 0 ||
		!strings.Contains(result.Approval.HumanInstruction, "human terminal") {
		t.Fatalf("status approval handoff = %#v", result.Approval)
	}
}

func TestStatus(t *testing.T) {
	root := tempRoot(t)
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "cache-ttl", "tester", "sample"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(mustStatePath(t, root, "cache-ttl"))
	result, err := Status(root, "cache-ttl")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(mustStatePath(t, root, "cache-ttl"))
	if result.Revision != 1 || string(before) != string(after) {
		t.Fatalf("status mutated state: %+v", result)
	}
}

func mustStatePath(t *testing.T, root, change string) string {
	t.Helper()
	path, err := mustOwner(t, root).ChangeState(change)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusReadinessTextJSONParity(t *testing.T) {
	root := statusReadinessRoot(t)
	first, err := Status(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Status(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated status differs:\n%#v\n%#v", first, second)
	}
	wantFrontier := []string{"ready"}
	if !reflect.DeepEqual(first.Frontier, wantFrontier) || !first.ApprovalStatus.Current {
		t.Fatalf("status frontier/approval = %#v", first)
	}
	if first.Counts != (ActivityCounts{Pending: 2, InProgress: 1, Completed: 1, Failed: 1, Blocked: 1}) {
		t.Fatalf("counts = %#v", first.Counts)
	}
	if first.Next.Kind != "operation" || first.Next.Operation != "next" {
		t.Fatalf("next = %#v", first.Next)
	}
	// One envelope feeds both surfaces, so the terminal and the JSON document
	// cannot disagree about a fact.
	envelope, err := Envelope(Outcome{Operation: "status", Value: first})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := agentjson.Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	text := RenderText(envelope)
	for _, value := range []string{
		"change: safe-change", "stage: approved", "approval_current: true",
		"frontier: ready", "next: operation next",
	} {
		if !strings.Contains(text, value) {
			t.Fatalf("text missing %q:\n%s", value, text)
		}
	}
	for _, value := range []string{
		`"stage":"approved"`, `"approval_current":true`, `"operation":"next"`,
	} {
		if !strings.Contains(string(raw), value) {
			t.Fatalf("json missing %s: %s", value, raw)
		}
	}
	// Per-task readiness rows are `report`'s projection, not a second one here.
	rows := factValue(mustReport(t, root, "safe-change", report.KindStatus), "tasks")
	for _, id := range []string{"ready", "active", "waiting", "failed", "blocked", "done"} {
		if !strings.Contains(rows, id+" activity=") {
			t.Fatalf("task %q missing from report rows: %s", id, rows)
		}
	}
}

func mustReport(t *testing.T, root, change, kind string) ReportResult {
	t.Helper()
	result, err := Report(root, change, kind, "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestStatusReadinessStaleAndInvalid(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		root := statusReadinessRoot(t)
		path := filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md")
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteString("\ndrift\n")
		_ = file.Close()
		result, err := Status(root, "safe-change")
		if err != nil {
			t.Fatal(err)
		}
		if result.ApprovalStatus.Current || len(result.Frontier) != 0 ||
			result.Next.Kind != "human_handoff" || result.Next.Owner != "human" {
			t.Fatalf("stale result = %#v", result)
		}
		// The handoff reaches the agent as the one legal next action, never as
		// a second approval document it could act on.
		envelope, err := Envelope(Outcome{Operation: "status", Value: result})
		if err != nil {
			t.Fatal(err)
		}
		if envelope.Next.Kind != agentjson.NextHumanHandoff || envelope.Next.Owner != "human" ||
			!strings.Contains(envelope.Next.Instruction, "human approval") {
			t.Fatalf("stale next = %#v", envelope.Next)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		root := tempRoot(t)
		if _, err := Init(root); err != nil {
			t.Fatal(err)
		}
		if _, err := New(root, "broken-change", "tester", "sample"); err != nil {
			t.Fatal(err)
		}
		result, err := Status(root, "broken-change")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Frontier) != 0 || result.Next.Kind != "blocked" ||
			result.Next.Owner != "author" {
			t.Fatalf("invalid result = %#v", result)
		}
	})
}

func TestStatusReadinessEmptyComplete(t *testing.T) {
	model := core.ProjectReadiness(
		statusEmptyTasks(), nil, "approved", core.ApprovalStatus{Current: true},
	)
	next := statusNextAction("empty-change", model, nil)
	if !model.AllComplete() || len(model.Frontier()) != 0 ||
		next.Kind != "terminal" || next.Action != "all task work is complete" {
		t.Fatalf("empty model = %#v next=%#v", model, next)
	}
}

func statusReadinessRoot(t *testing.T) string {
	t.Helper()
	root := tempRoot(t)
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "safe-change", "tester", "sample"); err != nil {
		t.Fatal(err)
	}
	change := filepath.Join(root, ".specd", "changes", "safe-change")
	writeStatusFile(t, filepath.Join(change, "proposal.md"), []byte(
		"## Problem\nUpdates race.\n## Outcome\nUpdates serialize.\n## Scope\nLocal updates.\n## Non-goals\nDistributed locks.\n## Affected capabilities\nsample\n"))
	writeStatusFile(t, filepath.Join(change, "design.md"), []byte(
		"## Boundaries\nsample/Requirement: Stable updates\n## Interfaces\nOne transaction.\n## Invariants\nOne writer.\n## Failure behavior\nNo partial write.\n## Integration\nExisting owner.\n## Alternatives\nNo distributed lock.\n## Owner\ninternal/sample\n"))
	writeStatusFile(t, filepath.Join(change, "tasks.md"), []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n"+
			"|---|---|---|---|---|---|---|\n"+
			"| ready | builder | internal/ready.go | | sample/Requirement: Stable updates | `go test ./...` | Ready works |\n"+
			"| active | builder | internal/active.go | | sample/Requirement: Stable updates | `go test ./...` | Active works |\n"+
			"| waiting | builder | internal/waiting.go | active | sample/Requirement: Stable updates | `go test ./...` | Waiting works |\n"+
			"| failed | builder | internal/failed.go | | sample/Requirement: Stable updates | `go test ./...` | Failed works |\n"+
			"| blocked | builder | internal/blocked.go | | sample/Requirement: Stable updates | `go test ./...` | Blocked works |\n"+
			"| done | builder | internal/done.go | | sample/Requirement: Stable updates | `go test ./...` | Done works |\n"))
	writeStatusFile(t, filepath.Join(change, "specs", "sample", "spec.md"), []byte(
		"## Purpose\nSample updates.\n## ADDED Requirements\n### Requirement: Stable updates\nThe store MUST serialize updates.\n#### Scenario: Concurrent\n- **WHEN** updates race\n- **THEN** both commit\n"))
	statePath := mustStatePath(t, root, "safe-change")
	raw, _ := os.ReadFile(statePath)
	current, err := state.Decode(raw, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	current.Tasks = map[string]json.RawMessage{
		"active":  json.RawMessage(`"in_progress"`),
		"failed":  json.RawMessage(`"failed"`),
		"blocked": json.RawMessage(`"blocked"`),
		"done":    json.RawMessage(`"completed"`),
	}
	raw, err = state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	writeStatusFile(t, statePath, raw)
	_, err = core.Approve(root, "safe-change", core.ApproveIntent{
		GitEmail: "human@example.com", ClaimedApprover: "human@example.com",
		Reason: "reviewed", Route: core.ApprovalRouteHumanTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func statusEmptyTasks() plan.Tasks {
	return plan.Tasks{Tasks: []plan.Task{}}
}

func writeStatusFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
