package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestCheckCompleteReadOnly(t *testing.T) {
	root := checkRoot(t, true)
	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	historyPath := filepath.Join(root, ".specd", "history.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeHistory, _ := os.ReadFile(historyPath)

	result, err := RunCheck(root, "safe-change", DefaultPolicyDigest())
	if err != nil || !result.Success || len(result.Findings) != 0 {
		t.Fatalf("check = %#v, %v", result, err)
	}
	afterState, _ := os.ReadFile(statePath)
	afterHistory, _ := os.ReadFile(historyPath)
	if !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(beforeHistory, afterHistory) {
		t.Fatal("check mutated state or history")
	}
}

func TestCheckPartialReportsAll(t *testing.T) {
	root := checkRoot(t, false)
	result, err := RunCheck(root, "safe-change", DefaultPolicyDigest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || len(result.Findings) < 3 {
		t.Fatalf("partial check did not accumulate findings: %#v", result)
	}
	for _, finding := range result.Findings {
		if finding.Gate == "" || finding.Problem == "" || finding.Repair == "" {
			t.Fatalf("non-actionable finding: %#v", finding)
		}
	}
}

func TestCheckApprovalParityReusesSnapshotAfterDrift(t *testing.T) {
	root := checkRoot(t, false)
	snapshot, err := AssembleCheck(root, "safe-change", DefaultPolicyDigest())
	if err != nil {
		t.Fatal(err)
	}
	check := EvaluateCheck(snapshot)
	writeCheckFile(t, filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md"), []byte(
		"## Problem\nchanged after check\n",
	))
	approval := EvaluateCheck(snapshot)
	if !reflect.DeepEqual(check, approval) {
		t.Fatalf("approval saw different result: %#v != %#v", check, approval)
	}
}

func TestCheckRejectsNonCanonicalPolicy(t *testing.T) {
	root := checkRoot(t, true)
	if _, err := RunCheck(root, "safe-change", "not-the-default-policy"); err == nil {
		t.Fatal("check accepted a non-canonical policy digest")
	}
}

func TestCheckRejectsSymlinkState(t *testing.T) {
	root := checkRoot(t, true)
	change := filepath.Join(root, ".specd", "changes", "safe-change")
	statePath := filepath.Join(change, "state.json")
	outside := filepath.Join(root, "outside-state.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	writeCheckFile(t, outside, raw)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCheck(root, "safe-change", DefaultPolicyDigest()); err == nil {
		t.Fatal("check followed a symlinked state file")
	}
}

func TestCheckWarningsNeverMaskErrors(t *testing.T) {
	snapshot := CheckSnapshot{root: "/root", change: "change"}
	result := newCheckResult(snapshot, []gates.Finding{
		{Gate: "warning", Severity: gates.Warning, Problem: "warn", Repair: "repair warning"},
		{Gate: "error", Severity: gates.Error, Problem: "fail", Repair: "repair error"},
	})
	if result.Success || len(result.Findings) != 2 {
		t.Fatalf("warning masked error: %#v", result)
	}
}

func checkRoot(t *testing.T, complete bool) string {
	t.Helper()
	root := t.TempDir()
	change := filepath.Join(root, ".specd", "changes", "safe-change")
	for _, directory := range []string{
		change, filepath.Join(change, "specs", "sample"),
		filepath.Join(root, ".specd", "specs"), filepath.Join(root, ".specd", "archive"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	current := state.Initial("safe-change", "created-record")
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	writeCheckFile(t, filepath.Join(change, "state.json"), raw)
	writeCheckFile(t, filepath.Join(root, ".specd", "history.jsonl"), nil)
	writeCheckFile(t, filepath.Join(root, ".specd", "evidence.jsonl"), nil)
	if !complete {
		writeCheckFile(t, filepath.Join(change, "proposal.md"), []byte("## Problem\n<problem>\n"))
		return root
	}
	writeCheckFile(t, filepath.Join(change, "proposal.md"), []byte(
		"## Problem\nUpdates can race.\n## Outcome\nUpdates serialize.\n## Scope\nLocal updates.\n## Non-goals\nDistributed locks.\n## Affected capabilities\nsample\n"))
	writeCheckFile(t, filepath.Join(change, "design.md"), []byte(
		"## Boundaries\nsample/Requirement: Stable updates\n## Interfaces\nOne transaction.\n## Invariants\nOne writer.\n## Failure behavior\nNo partial write.\n## Integration\nExisting owner.\n## Alternatives\nNo distributed lock.\n## Owner\ninternal/sample\n"))
	writeCheckFile(t, filepath.Join(change, "tasks.md"), []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n|---|---|---|---|---|---|---|\n| T1 | builder | internal/sample.go | | sample/Requirement: Stable updates | `go test ./...` | Concurrent update check passes |\n"))
	writeCheckFile(t, filepath.Join(change, "specs", "sample", "spec.md"), []byte(
		"## Purpose\nSample updates.\n## ADDED Requirements\n### Requirement: Stable updates\nThe store MUST serialize updates.\n#### Scenario: Concurrent\n- **WHEN** updates race\n- **THEN** both commit\n"))
	return root
}

func writeCheckFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
