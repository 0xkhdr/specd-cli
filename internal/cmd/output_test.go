package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	contextmodel "github.com/0xkhdr/specd-cli/internal/context"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const (
	fixtureRoot   = "/project"
	fixtureChange = "safe-create"
	fixtureTask   = "S1-01"
	fixtureDigest = "6f4b6612125fb3a0daecd2799dfd6c9c299424fd920f9b308110a2c1fbd8f443"
	fixtureHead   = "0f1e2d3c4b5a69788796a5b4c3d2e1f001234567"
)

func statusFixture(next StatusNextAction, counts ActivityCounts, frontier []string, approved, done bool) StatusResult {
	result := StatusResult{
		Root:           Root{Path: fixtureRoot},
		ApprovalStatus: core.ApprovalStatus{Change: fixtureChange, Current: approved},
		Counts:         counts, Frontier: frontier, Next: next, AllTasksComplete: done,
		Projection: state.Projection{
			Change: fixtureChange, Schema: 1, Stage: "approved",
			Condition: "active", Revision: 4, NextAction: next.Action,
		},
	}
	if !approved {
		result.Stage = "planning"
		result.Approval = &core.ApprovalHandoff{
			HumanApprovalRequired: true, Gate: "planning_to_approved",
			HumanInstruction: "ask a human to run specd approve " + fixtureChange + " in a human terminal",
			Assurance:        core.ApprovalAssuranceAdvisory,
		}
	}
	return result
}

// fixtures are the stable ready, blocked, stale, verification-failed,
// completion-ready, and human-handoff results every surface must agree on.
func fixtures() map[string]Outcome {
	return map[string]Outcome{
		"status-ready": {Operation: "status", Value: statusFixture(
			StatusNextAction{Kind: "operation", Operation: "next",
				Action: "run specd next " + fixtureChange},
			ActivityCounts{Pending: 2}, []string{fixtureTask, "S1-02"}, true, false)},
		"status-human-handoff": {Operation: "status", Value: statusFixture(
			StatusNextAction{Kind: "human_handoff", Owner: "human",
				Action: "ask a human to run specd approve " + fixtureChange + " in a human terminal"},
			ActivityCounts{Pending: 2}, nil, false, false)},
		"status-terminal": {Operation: "status", Value: statusFixture(
			StatusNextAction{Kind: "terminal", Action: "all task work is complete"},
			ActivityCounts{Completed: 2}, nil, true, true)},
		"next-blocked": {Operation: "next", Value: NextResult{
			Root: Root{Path: fixtureRoot}, Change: fixtureChange, Revision: 4,
			Frontier: []string{}, Classification: "blocked",
			Blocker: &core.ReadinessBlocker{
				Code: "dependency_incomplete", Owner: "author",
				Action: "complete the blocking task first",
			},
			Action: "complete the blocking task first",
		}},
		"context-success": {Operation: "context", Value: contextmodel.Manifest{
			Version: "1", Root: fixtureRoot, Change: fixtureChange, Task: fixtureTask,
			Role: "implementer", StateRevision: 4, ApprovalHash: fixtureDigest,
			Frontier: []string{fixtureTask}, FrontierHash: fixtureDigest,
			Items: []contextmodel.ManifestItem{{
				Kind: "task", Path: "tasks.md", Location: plan.Location{Path: "tasks.md", Line: 12},
				Digest: fixtureDigest, Content: "secret plan text that must never ship",
			}},
			RequiredBytes: 512, ManifestHash: fixtureDigest,
			Authority: contextmodel.Authority{
				AllowedWritePaths: []string{"internal/core/scope.go"},
				OperationClass:    "implement", Verify: "go test ./internal/core",
				Assurance: "advisory",
			},
		}},
		"verify-failed": {Operation: "verify", Root: fixtureRoot, Value: VerifyResult{
			RecordID: "ev-000007",
			Evidence: evidence.TestRun{
				SchemaVersion: 1, Class: evidence.ClassTestRun, Change: fixtureChange,
				TaskID: fixtureTask, AttemptID: "A1", HEAD: fixtureHead,
				StateRevision: 6, ExitCode: 1, Passed: false, StdoutDigest: fixtureDigest,
				StderrDigest: fixtureDigest, StdoutCut: true,
			},
		}},
		"verify-passed": {Operation: "verify", Root: fixtureRoot, Value: VerifyResult{
			RecordID: "ev-000008",
			Evidence: evidence.TestRun{
				SchemaVersion: 1, Class: evidence.ClassTestRun, Change: fixtureChange,
				TaskID: fixtureTask, AttemptID: "A1", HEAD: fixtureHead,
				StateRevision: 6, ExitCode: 0, Passed: true, NonVacuous: true,
				StdoutDigest: fixtureDigest, StderrDigest: fixtureDigest,
			},
		}},
		"complete-success": {Operation: "complete", Root: fixtureRoot, Value: core.Completion{
			SchemaVersion: 1, Change: fixtureChange, TaskID: fixtureTask, AttemptID: "A1",
			EvidenceID: "ev-000008", RevisionBefore: 6, RevisionAfter: 7, HistoryID: "h-000009",
		}},
		"check-failed": {Operation: "check", Value: core.CheckResult{
			Root: fixtureRoot, Change: fixtureChange, StateRevision: 4,
			RegistryVersion: "1", PolicyDigest: fixtureDigest, Success: false,
			Findings: []gates.Finding{{
				Gate: "tasks_declare_files", Severity: gates.Error,
				Location: plan.Location{Path: "tasks.md", Line: 12},
				Problem:  "task S1-01 declares no files",
				Repair:   "declare the files the task may write",
			}},
		}},
		"check-passed": {Operation: "check", Value: core.CheckResult{
			Root: fixtureRoot, Change: fixtureChange, StateRevision: 4,
			RegistryVersion: "1", PolicyDigest: fixtureDigest, Success: true,
		}},
		"refusal-stale-evidence": {
			Operation: "complete", Root: fixtureRoot, Change: fixtureChange, Task: fixtureTask,
			Err: failure.New("evidence_stale", fixtureRoot, "",
				"no applicable passing evidence at current HEAD",
				fmt.Sprintf("run specd verify %s %s <attempt>", fixtureChange, fixtureTask)),
		},
		"refusal-usage": {
			Operation: "context",
			Err: &UsageRefusal{Refusal: failure.New("flag_unknown", "", "",
				`unknown flag "--all" for context`, "run specd context <change> <task>")},
		},
		"refusal-human-boundary": {
			Operation: "approve", Root: fixtureRoot, Change: fixtureChange,
			Err: &UsageRefusal{Refusal: failure.New("human_approval_required", fixtureRoot, "",
				"approve is human-only and cannot use an agent-capable route",
				"hand off approval to a human terminal")},
		},
	}
}

func TestAgentJSONGolden(t *testing.T) {
	for name, outcome := range fixtures() {
		t.Run(name, func(t *testing.T) {
			encoded, exit, err := RenderJSON(outcome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			repeated, repeatedExit, err := RenderJSON(outcome)
			if err != nil || string(repeated) != string(encoded) || repeatedExit != exit {
				t.Fatalf("rendering is not byte stable: %v", err)
			}
			golden := filepath.Join("testdata", "agent-json", name+".golden")
			want := readGolden(t, golden, string(encoded))
			if string(encoded) != want {
				t.Fatalf("envelope drifted from %s:\nwant %s\ngot  %s", golden, want, encoded)
			}
			envelope, err := agentjson.Decode(encoded)
			if err != nil {
				t.Fatalf("golden envelope is invalid: %v", err)
			}
			if envelope.Exit.Code != exit {
				t.Fatalf("exit %d disagrees with envelope %d", exit, envelope.Exit.Code)
			}
			assertTextParity(t, envelope)
		})
	}
}

// assertTextParity proves the terminal surface states the same facts,
// diagnostics, exit, and single legal next action as the document.
func assertTextParity(t *testing.T, envelope agentjson.Envelope) {
	t.Helper()
	rendered := RenderText(envelope)
	require := []string{
		"operation: " + envelope.Operation,
		fmt.Sprintf("ok: %t", envelope.OK),
		fmt.Sprintf("exit: %d %s", envelope.Exit.Code, envelope.Exit.Class),
		envelope.Next.Kind, envelope.Next.Instruction,
	}
	if envelope.Root != nil {
		require = append(require, "root: "+envelope.Root.Path)
	}
	if envelope.Subject != nil {
		require = append(require, "change: "+envelope.Subject.Change)
	}
	if envelope.State != nil {
		require = append(require, fmt.Sprintf("revision: %d", envelope.State.Revision))
	}
	for key, value := range envelope.Data {
		require = append(require, key+": "+text(value))
	}
	for _, diagnostic := range envelope.Diagnostics {
		require = append(require, diagnostic.Code, diagnostic.Message, diagnostic.Fix)
	}
	for _, fragment := range require {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("text output omits %q:\n%s", fragment, rendered)
		}
	}
	if strings.Contains(rendered, "specd approve") != strings.Contains(string(mustEncode(t, envelope)), "specd approve") {
		t.Fatalf("text and JSON disagree about the approval boundary:\n%s", rendered)
	}
}

func mustEncode(t *testing.T, envelope agentjson.Envelope) []byte {
	t.Helper()
	encoded, err := agentjson.Encode(envelope)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return encoded
}

// TestAgentJSONNeverOffersApprove proves the human gate stays uncallable on the
// machine surface no matter which fixture produced the envelope.
func TestAgentJSONNeverOffersApprove(t *testing.T) {
	for name, outcome := range fixtures() {
		encoded, _, err := RenderJSON(outcome)
		if err != nil {
			t.Fatalf("%s: render: %v", name, err)
		}
		envelope, err := agentjson.Decode(encoded)
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if envelope.Next.Operation == "approve" {
			t.Fatalf("%s offers a callable approve", name)
		}
	}
}

// TestAgentJSONHidesSourceText proves bounded context facts ship without the
// source content the manifest carries.
func TestAgentJSONHidesSourceText(t *testing.T) {
	encoded, _, err := RenderJSON(fixtures()["context-success"])
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(encoded), "secret plan text") {
		t.Fatalf("manifest content leaked into the envelope: %s", encoded)
	}
}

func TestAgentJSONExitClasses(t *testing.T) {
	// cmd.ExitCode is the single exit-class owner: a pre-handler refusal is
	// class 2, and a refusal raised inside a handler — stale evidence here —
	// is class 1. The renderer projects that mapping; it does not re-decide it.
	want := map[string]int{
		"status-ready": 0, "check-failed": 1, "verify-failed": 1, "verify-passed": 0,
		"refusal-usage": 2, "refusal-stale-evidence": 1, "refusal-human-boundary": 2,
	}
	all := fixtures()
	for name, code := range want {
		_, exit, err := RenderJSON(all[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if exit != code {
			t.Fatalf("%s exited %d, want %d", name, exit, code)
		}
	}
}

func TestAgentJSONPlainErrorKeepsOneNextAction(t *testing.T) {
	encoded, exit, err := RenderJSON(Outcome{
		Operation: "start", Root: fixtureRoot, Change: fixtureChange, Task: fixtureTask,
		Err: errors.New("start actor is required; next: retry through an authorized harness operation"),
	})
	if err != nil || exit != 1 {
		t.Fatalf("render: %v exit %d", err, exit)
	}
	envelope, err := agentjson.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Diagnostics) != 1 ||
		envelope.Diagnostics[0].Fix != "retry through an authorized harness operation" {
		t.Fatalf("plain error lost its one recovery: %s", encoded)
	}
	if strings.Contains(envelope.Diagnostics[0].Message, "next:") {
		t.Fatalf("message repeats the next action: %s", encoded)
	}
}

func readGolden(t *testing.T, path, actual string) string {
	t.Helper()
	if os.Getenv("SPECD_WRITE_AGENT_JSON") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with SPECD_WRITE_AGENT_JSON=1)", path, err)
	}
	return string(raw)
}
