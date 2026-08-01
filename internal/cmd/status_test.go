package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
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
	raw, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(raw), `"humanApprovalRequired":true`) {
		t.Fatalf("status JSON = %s, %v", raw, err)
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
	raw, err := RenderStatusJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	var decoded StatusResult
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(decoded, first) {
		t.Fatalf("JSON parity = %#v, %v", decoded, err)
	}
	text := RenderStatusText(first)
	for _, value := range []string{
		"lifecycle: approved", "approval-current: true",
		"task ready:", "task active:", "task waiting:",
		"task failed:", "task blocked:", "task done:",
		"frontier: ready", "next: kind=operation operation=next",
	} {
		if !strings.Contains(text, value) {
			t.Fatalf("text missing %q:\n%s", value, text)
		}
	}
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

// Canonical status carries the stage-8 facts a reviewer needs — the policy it
// ran under, the assurance it can honestly claim, whether a review packet is
// approvable, and mechanical deferred-domain eligibility — on both surfaces.
func TestStatusProduction(t *testing.T) {
	const change, taskID = "sample-loop", "edit-sample"
	root, _ := productionAttempt(t, change, taskID, "agent:builder")

	result, err := Status(root, change)
	if err != nil {
		t.Fatal(err)
	}
	production := result.Production
	if production.Profile != string(core.ProfileDefault) ||
		production.PolicyDigest != core.DefaultPolicyDigest() ||
		production.Assurance != core.AttemptAssurance {
		t.Fatalf("status production = %#v", production)
	}
	// Nothing is proven yet, so the packet is not approvable and says why.
	if production.ReviewApprovable || len(production.ReviewBlockers) == 0 {
		t.Fatalf("unproven change presented an approvable packet: %#v", production)
	}
	// Eligibility is mechanical and empty until two independent records exist;
	// it never authorizes a deferred domain.
	if len(production.FrictionEligibility) != 0 {
		t.Fatalf("friction eligibility = %#v with no records", production.FrictionEligibility)
	}

	text := RenderStatusText(result)
	for _, want := range []string{
		"profile: " + production.Profile,
		"policy-digest: " + production.PolicyDigest,
		"assurance: " + production.Assurance,
		"review-approvable: false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text is missing %q:\n%s", want, text)
		}
	}
	raw, err := RenderStatusJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded StatusResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Production, production) {
		t.Fatalf("json production = %#v, want %#v", decoded.Production, production)
	}
}
