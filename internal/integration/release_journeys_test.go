// Release journeys are the stage-9 qualification runner: they replay the base
// loop and every required refusal/recovery route over isolated fixtures, using
// the same CLI routes a caller uses. Nothing here writes harness-owned state,
// history, evidence, or task activity: every persisted fact a journey asserts
// was produced by the harness during that journey.
package integration

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/generate"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const (
	releaseChange = "release-sample"
	releaseTask   = "edit-sample"
	releaseHuman  = "human@example.com"
	afterSample   = "package sample\n\nfunc Sample() string { return \"after\" }\n"
)

// TestReleaseJourneyFreshProject is journey 1 on its own: a fresh project runs
// the whole base loop from init to archive.
func TestReleaseJourneyFreshProject(t *testing.T) {
	releaseStdin(t)
	releaseFreshProject(t)
}

// TestReleaseJourneys replays every stage-9 required journey. Each is named for
// its entry in requiredJourneys (release_test.go) and none may be skipped: a
// journey that cannot run fails rather than narrowing the release claim.
func TestReleaseJourneys(t *testing.T) {
	releaseStdin(t)
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		// Platform facts are observed, never inferred, and an unrun platform is
		// reported as unsupported rather than assumed to pass.
		t.Fatalf("release journeys are qualified on linux, darwin, and windows only; this run is %s", runtime.GOOS)
	}
	t.Logf("release journeys platform: %s/%s", runtime.GOOS, runtime.GOARCH)

	t.Run("01 fresh project and one-task change", func(t *testing.T) { releaseFreshProject(t) })
	t.Run("02 brownfield capability delta", releaseBrownfieldJourney)
	t.Run("03 two wave-0 tasks and a dependent wave-1 task", releaseWaveJourney)
	t.Run("04 interruption after state mutation and after evidence append", releaseInterruptionJourney)
	t.Run("05 malformed Markdown and corrupt or future state", releaseMalformedJourney)
	t.Run("06 stale approval after artifact byte change", releaseStaleApprovalJourney)
	t.Run("07 stale evidence after Git HEAD change", releaseStaleEvidenceJourney)
	t.Run("08 out-of-scope implementation diff", releaseOutOfScopeJourney)
	t.Run("09 failing and zero-match verification", releaseVerificationJourney)
	t.Run("10 sync conflict and injected multi-file write failure", releaseSyncFaultJourney)
	t.Run("11 archive target collision", releaseArchiveCollisionJourney)
	t.Run("12 agent handoff at the human gate", releaseHandoffJourney)
	t.Run("13 default and production profile comparison", releaseProfileJourney)
	t.Run("14 fresh agent resume from repository bytes", releaseResumeJourney)
}

// TestFreshAgentResume derives the nine fresh-agent facts from repository bytes
// and canonical projections only, and asserts the projections agree.
func TestFreshAgentResume(t *testing.T) {
	releaseStdin(t)
	releaseResumeJourney(t)
}

// ------------------------------------------------------------------ journeys

func releaseFreshProject(t *testing.T) release {
	t.Helper()
	r, synced := releaseSynced(t)
	archived := r.json("archive", releaseChange)
	target := dataString(t, archived, "target")
	if target != dataString(t, synced, "archive_target") {
		t.Fatalf("archive target %q, sync announced %q", target, dataString(t, synced, "archive_target"))
	}
	if _, err := os.Stat(filepath.Join(r.root, ".specd", "changes", releaseChange)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("archived change is still active: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, ".specd", filepath.FromSlash(target), "tasks.md")); err != nil {
		t.Fatalf("archived artifacts are not inspectable: %v", err)
	}
	return r
}

// releaseSynced runs the base loop up to and including the human sync gate, so
// journeys that need reconciled accepted truth do not need to archive it.
func releaseSynced(t *testing.T) (release, map[string]any) {
	t.Helper()
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()

	r.must("next", releaseChange)
	manifest := r.json("context", releaseChange, releaseTask)
	if declared := stringsOf(manifest, "allowed_write_paths"); !slices.Equal(declared, []string{"sample.go"}) {
		t.Fatalf("declared write paths = %v", declared)
	}
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	attempt := dataString(t, started, "attempt")

	releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
	verified := r.json("verify", releaseChange, releaseTask, attempt)
	if dataOf(verified)["passed"] != true {
		t.Fatalf("verify = %v", dataOf(verified))
	}
	completed := r.json("complete", releaseChange, releaseTask, "--revision", revisionOf(t, verified))
	// Completion consumes exactly the evidence this journey recorded.
	if completed := dataOf(completed)["evidence"]; completed != dataOf(verified)["record_id"] {
		t.Fatalf("completion consumed %v, verification recorded %v", completed, dataOf(verified)["record_id"])
	}

	synced := r.sync()
	accepted := readFile(t, filepath.Join(r.root, ".specd", "specs", "sample", "spec.md"))
	if !strings.Contains(accepted, "Requirement: Current greeting") {
		t.Fatalf("accepted specs omit the reconciled requirement:\n%s", accepted)
	}
	return r, synced
}

// releaseBrownfieldJourney proves a capability delta against behavior that is
// already accepted: the second change modifies the requirement the first one
// synced rather than adding it.
func releaseBrownfieldJourney(t *testing.T) {
	r := releaseFreshProject(t)
	const second = "release-brownfield"
	r.must("new", second, "--capability", "sample")
	dir := ".specd/changes/" + second + "/"
	releaseWrite(t, r.root, map[string]string{
		dir + "proposal.md": readFile(t, filepath.Join("testdata", "release", "fresh-project", "plan", "proposal.md")),
		dir + "design.md":   readFile(t, filepath.Join("testdata", "release", "fresh-project", "plan", "design.md")),
		dir + "specs/sample/spec.md": "## MODIFIED Requirements\n### Requirement: Current greeting\n" +
			"The sample package MUST return the current greeting exactly once.\n" +
			"#### Scenario: Greeting requested\n- **WHEN** the greeting is requested\n" +
			"- **THEN** the current greeting is returned\n",
		dir + "tasks.md": releaseTaskTable(
			"| edit-sample | builder | sample.go | | sample/Requirement: Current greeting | " +
				"`go test . -run '^TestSampleLoop$' -count=1` | The greeting is current and its test passes |"),
	})
	r.must("check", second)
	r.approveChange(second)
	manifest := r.json("context", second, releaseTask)
	started := r.json("start", second, releaseTask, "--revision", revisionOf(t, manifest))
	releaseWrite(t, r.root, map[string]string{"sample.go": afterSample + "\n// brownfield\n"})
	verified := r.json("verify", second, releaseTask, dataString(t, started, "attempt"))
	r.json("complete", second, releaseTask, "--revision", revisionOf(t, verified))
	r.syncChange(second)
	accepted := readFile(t, filepath.Join(r.root, ".specd", "specs", "sample", "spec.md"))
	if !strings.Contains(accepted, "exactly once") {
		t.Fatalf("accepted specs did not take the modification:\n%s", accepted)
	}
}

// releaseWaveJourney proves the dependency frontier: two independent wave-0
// tasks are ready together, and the wave-1 task becomes ready only after both
// complete.
func releaseWaveJourney(t *testing.T) {
	r := newRelease(t,
		map[string]string{"sample_test.go": "package sample\n\nimport \"testing\"\n\n" +
			"func TestSampleLoop(t *testing.T) {\n\tif Sample() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n"},
		map[string]string{".specd/changes/" + releaseChange + "/tasks.md": releaseTaskTable(
			releaseTaskRow("alpha", "alpha.go", ""),
			releaseTaskRow("beta", "beta.go", ""),
			releaseTaskRow("gamma", "gamma.go", "alpha; beta"))})
	r.must("check", releaseChange)
	r.approve()

	frontier := stringsOf(r.json("next", releaseChange), "frontier")
	if !slices.Equal(frontier, []string{"alpha", "beta"}) {
		t.Fatalf("wave-0 frontier = %v", frontier)
	}
	// The dependent task is not selectable while its dependencies are open.
	r.refuses([]string{"next", releaseChange, "gamma"}, "dependency_incomplete")
	releaseFrictionRoute(t, r)

	for _, task := range []string{"alpha", "beta"} {
		r.runTask(releaseChange, task, task+".go", "package sample\n\nvar "+task+" = 1\n")
	}
	if frontier = stringsOf(r.json("next", releaseChange), "frontier"); !slices.Equal(frontier, []string{"gamma"}) {
		t.Fatalf("wave-1 frontier = %v", frontier)
	}
	r.runTask(releaseChange, "gamma", "gamma.go", "package sample\n\nvar gamma = 1\n")
	if r.json("status", releaseChange)["data"].(map[string]any)["all_tasks_complete"] != true {
		t.Fatal("all tasks did not complete")
	}
}

// releaseFrictionRoute proves the D14 observation route over the blocked task
// this journey already owns: friction is recordable only against a task the
// readiness owner reports as blocked, it appends evidence without moving any
// authority, and a repeated observation of the same change and task can never
// manufacture the second independent record eligibility requires.
func releaseFrictionRoute(t *testing.T, r release) {
	t.Helper()
	before := revisionOf(t, r.json("status", releaseChange))

	// A ready task, an undeferred domain, and a stale observer each refuse.
	r.refuses([]string{"friction", releaseChange, "alpha", "--domain", "orchestration",
		"--blocked-operation", "next", "--consequence", "none", "--revision", before},
		"friction_hypothetical")
	r.refuses([]string{"friction", releaseChange, "gamma", "--domain", "readiness",
		"--blocked-operation", "next", "--consequence", "none", "--revision", before},
		"flag_enum_unknown")
	r.refuses([]string{"friction", releaseChange, "gamma", "--domain", "orchestration",
		"--blocked-operation", "next", "--consequence", "none", "--revision", "999"},
		"stale_revision")

	recorded := dataOf(r.json("friction", releaseChange, "gamma",
		"--domain", "orchestration", "--blocked-operation", "next",
		"--consequence", "the dependent task cannot be scheduled by hand",
		"--revision", before))
	if recorded["domain"] != "orchestration" || recorded["record_count"] != float64(1) {
		t.Fatalf("friction record = %v", recorded)
	}
	// One observation is never eligibility, and friction unblocks nothing.
	if recorded["eligible"] != false {
		t.Fatalf("one friction record must not be eligible: %v", recorded)
	}
	if after := revisionOf(t, r.json("status", releaseChange)); after != before {
		t.Fatalf("friction moved the revision from %s to %s", before, after)
	}
	r.refuses([]string{"next", releaseChange, "gamma"}, "dependency_incomplete")

	// The same change and task observed twice is one fact, not two.
	repeated := dataOf(r.json("friction", releaseChange, "gamma",
		"--domain", "orchestration", "--blocked-operation", "context",
		"--consequence", "observed again from the same blocked task",
		"--revision", before))
	if repeated["record_count"] != float64(1) || repeated["eligible"] != false {
		t.Fatalf("repeated observation manufactured eligibility: %v", repeated)
	}
}

// releaseInterruptionJourney kills nothing: each CLI invocation is its own
// process boundary, so the durable bytes left after a state mutation and after
// an evidence append are exactly what a crashed process would leave behind.
func releaseInterruptionJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	manifest := r.json("context", releaseChange, releaseTask)
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	attempt := dataString(t, started, "attempt")

	// Boundary one: state was mutated, no evidence exists. A later process sees
	// the durable revision and invents no completion.
	after := r.json("status", releaseChange)
	if revisionOf(t, after) != revisionOf(t, started) {
		t.Fatalf("revision after interruption = %s, want %s", revisionOf(t, after), revisionOf(t, started))
	}
	if dataOf(after)["completed"] != float64(0) {
		t.Fatalf("interrupted attempt was counted complete: %v", dataOf(after))
	}
	if releaseRecords(t, r.root, "evidence.jsonl") != 0 {
		t.Fatal("evidence exists before any verification ran")
	}

	// Boundary two: evidence was appended, completion has not run. The record
	// survives, and the task is still open.
	releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
	verified := r.json("verify", releaseChange, releaseTask, attempt)
	if releaseRecords(t, r.root, "evidence.jsonl") != 1 {
		t.Fatal("verification did not durably append exactly one record")
	}
	if dataOf(r.json("status", releaseChange))["in_progress"] != float64(1) {
		t.Fatal("evidence alone closed the task")
	}
	// Recovery continues from the durable prefix instead of restarting.
	r.json("complete", releaseChange, releaseTask, "--revision", revisionOf(t, verified))
	if dataOf(r.json("status", releaseChange))["completed"] != float64(1) {
		t.Fatal("recovery could not complete the interrupted task")
	}
}

func releaseMalformedJourney(t *testing.T) {
	t.Run("malformed Markdown", func(t *testing.T) {
		r := newRelease(t, nil, map[string]string{
			".specd/changes/" + releaseChange + "/tasks.md": "this is not a task table\n",
		})
		exit, document := r.jsonAny("check", releaseChange)
		if exit == 0 {
			t.Fatal("malformed tasks.md checked clean")
		}
		if len(releaseDiagnostics(t, document)) == 0 {
			t.Fatalf("no diagnostic named the malformed artifact: %v", document)
		}
		releaseOneNextAction(t, document)
	})
	t.Run("corrupt and future state", func(t *testing.T) {
		statePath := ".specd/changes/" + releaseChange + "/state.json"
		for name, bytes := range map[string]string{
			"future":  `{"schemaVersion":99,"change":"release-sample","stage":"planning","condition":"active","revision":1,"approvals":[],"tasks":{},"createdBy":"x","lastTransition":"x"}`,
			"corrupt": "{not json",
		} {
			t.Run(name, func(t *testing.T) {
				r := newRelease(t, nil, nil)
				releaseWrite(t, r.root, map[string]string{statePath: bytes})
				r.refuses([]string{"status", releaseChange}, "state_schema_unsupported", "state_corrupt")
			})
		}
	})
}

func releaseStaleApprovalJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	if dataOf(r.json("status", releaseChange))["approval_current"] != true {
		t.Fatal("approval is not current immediately after approval")
	}
	// One byte of authored planning truth changes, so the human authorization
	// no longer covers the current bytes.
	releaseWrite(t, r.root, map[string]string{
		".specd/changes/" + releaseChange + "/proposal.md": readFile(t,
			filepath.Join(r.root, ".specd", "changes", releaseChange, "proposal.md")) + "\nExtra scope.\n",
	})
	status := r.json("status", releaseChange)
	if dataOf(status)["approval_current"] != false {
		t.Fatalf("approval survived an artifact byte change: %v", dataOf(status))
	}
	if next := releaseOneNextAction(t, status); next["kind"] != "human_handoff" {
		t.Fatalf("stale approval offered %v instead of the human gate", next)
	}
	r.refuses([]string{"context", releaseChange, releaseTask}, "context_approval_stale")
}

func releaseStaleEvidenceJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	manifest := r.json("context", releaseChange, releaseTask)
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
	verified := r.json("verify", releaseChange, releaseTask, dataString(t, started, "attempt"))

	// HEAD moves after the observation, so the evidence no longer describes the
	// current bytes and completion refuses rather than consuming it.
	git(t, r.root, []string{"add", "-A"}, []string{"commit", "-m", "moved", "--no-gpg-sign"})
	r.refuses([]string{"complete", releaseChange, releaseTask, "--revision", revisionOf(t, verified)},
		"complete_head", "scope_drift", "complete_evidence")
}

func releaseOutOfScopeJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	manifest := r.json("context", releaseChange, releaseTask)
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	releaseWrite(t, r.root, map[string]string{
		"sample.go": afterSample,
		"other.go":  "package sample\n\nvar Other = 1\n",
	})
	document := r.refuses(
		[]string{"verify", releaseChange, releaseTask, dataString(t, started, "attempt")}, "scope_outside")
	if !strings.Contains(releaseDiagnostics(t, document)[0]["message"].(string), "other.go") {
		t.Fatalf("the refusal does not name the undeclared file: %v", document)
	}
	if releaseRecords(t, r.root, "evidence.jsonl") != 0 {
		t.Fatal("an out-of-scope attempt recorded evidence")
	}
}

func releaseVerificationJourney(t *testing.T) {
	t.Run("failing verification", func(t *testing.T) {
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		r.approve()
		manifest := r.json("context", releaseChange, releaseTask)
		started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
		// The declared file is edited, but not into a passing state.
		releaseWrite(t, r.root, map[string]string{
			"sample.go": "package sample\n\nfunc Sample() string { return \"still before\" }\n"})
		exit, document := r.jsonAny("verify", releaseChange, releaseTask, dataString(t, started, "attempt"))
		if exit == 0 || dataOf(document)["passed"] != false {
			t.Fatalf("failing verification reported %d: %v", exit, dataOf(document))
		}
		releaseOneNextAction(t, document)
		r.refuses([]string{"complete", releaseChange, releaseTask, "--revision", revisionOf(t, document)},
			"complete_evidence")
	})
	t.Run("zero-match verification", func(t *testing.T) {
		r := newRelease(t, nil, map[string]string{
			".specd/changes/" + releaseChange + "/tasks.md": releaseTaskTable(
				"| " + releaseTask + " | builder | sample.go | | sample/Requirement: Current greeting | " +
					"`go test . -run '^TestNothingMatchesThis$' -count=1` | The greeting is current and its test passes |"),
		})
		r.must("check", releaseChange)
		r.approve()
		manifest := r.json("context", releaseChange, releaseTask)
		started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
		releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
		exit, document := r.jsonAny("verify", releaseChange, releaseTask, dataString(t, started, "attempt"))
		if dataOf(document)["zero_match"] != true || dataOf(document)["passed"] != false {
			t.Fatalf("zero-match run reported %d: %v", exit, dataOf(document))
		}
		r.refuses([]string{"complete", releaseChange, releaseTask, "--revision", revisionOf(t, document)},
			"complete_evidence")
	})
}

func releaseSyncFaultJourney(t *testing.T) {
	t.Run("sync conflict", func(t *testing.T) {
		r, _ := releaseSynced(t)
		// Accepted truth drifts after the sync that wrote it. Re-running sync
		// refuses instead of overwriting a document it no longer owns.
		releaseWrite(t, r.root, map[string]string{
			".specd/specs/sample/spec.md": "## Purpose\nHand-edited.\n"})
		code, out := r.humanRun("sync", releaseChange, "--approver", releaseHuman, "--reason", "retry")
		if code == 0 || !strings.Contains(out, "sync_drift") {
			t.Fatalf("conflicting sync exited %d: %s", code, out)
		}
	})
	t.Run("injected multi-file write failure", func(t *testing.T) {
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		r.approve()
		r.runTask(releaseChange, releaseTask, "sample.go", afterSample)

		statePath := filepath.Join(r.root, ".specd", "changes", releaseChange, "state.json")
		acceptedPath := filepath.Join(r.root, ".specd", "specs", "sample", "spec.md")
		before := readFile(t, statePath)
		// Sync writes the accepted spec and the change state in one transaction.
		// The fault lands between them, which is the only interesting boundary.
		_, err := core.Sync(r.root, releaseChange, core.SyncOptions{
			GitEmail: releaseHuman, ClaimedApprover: releaseHuman, Reason: "injected fault",
			Route: core.ApprovalRouteHumanTerminal, Now: time.Now(),
			Hook: func(step string) error {
				if strings.HasPrefix(step, "before-replace:") && strings.HasSuffix(step, "state.json") {
					return errors.New("injected write failure")
				}
				return nil
			},
		})
		if err == nil {
			t.Fatal("sync committed through an injected write failure")
		}
		// Old or new, never a third state: the next process resolves the
		// interruption and offers exactly one legal action.
		exit, document := r.jsonAny("status", releaseChange)
		releaseOneNextAction(t, document)
		if exit != 0 {
			t.Fatalf("an interrupted sync left unreadable state: %v", document)
		}
		switch stage := dataOf(document)["stage"]; stage {
		case "approved":
			if readFile(t, statePath) != before {
				t.Fatal("a rolled-back sync left mutated state")
			}
			if _, err := os.Stat(acceptedPath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a rolled-back sync published accepted truth: %v", err)
			}
		case "reconciling":
			if accepted := readFile(t, acceptedPath); !strings.Contains(accepted, "Requirement: Current greeting") {
				t.Fatalf("a committed sync left incomplete accepted truth:\n%s", accepted)
			}
		default:
			t.Fatalf("interrupted sync left lifecycle %v", stage)
		}
	})
}

func releaseArchiveCollisionJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	manifest := r.json("context", releaseChange, releaseTask)
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
	verified := r.json("verify", releaseChange, releaseTask, dataString(t, started, "attempt"))
	r.json("complete", releaseChange, releaseTask, "--revision", revisionOf(t, verified))
	synced := r.sync()

	collision := filepath.Join(r.root, ".specd", filepath.FromSlash(dataString(t, synced, "archive_target")))
	if err := os.MkdirAll(collision, 0o755); err != nil {
		t.Fatal(err)
	}
	r.refuses([]string{"archive", releaseChange}, "archive_target_exists")
	// The change is untouched: the collision refused before any move.
	if _, err := os.Stat(filepath.Join(r.root, ".specd", "changes", releaseChange, "tasks.md")); err != nil {
		t.Fatalf("a refused archive moved the change: %v", err)
	}
}

func releaseHandoffJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	checked := r.json("check", releaseChange)
	next := releaseOneNextAction(t, checked)
	if next["kind"] != "human_handoff" || next["operation"] != nil {
		t.Fatalf("check handed the agent the gate: %v", next)
	}
	// Naming the verb does not open it, and no state moved.
	document := r.refuses([]string{"approve", releaseChange}, "human_approval_required", "human_operation_required")
	if releaseOneNextAction(t, document)["kind"] != "human_handoff" {
		t.Fatalf("agent approval was not a handoff: %v", document)
	}
	if dataOf(r.json("status", releaseChange))["approval_current"] != false {
		t.Fatal("an agent invocation recorded approval")
	}
	// Only the human route passes it.
	r.approve()
	if dataOf(r.json("status", releaseChange))["approval_current"] != true {
		t.Fatal("the human route did not pass the gate")
	}
}

func releaseProfileJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	// Both profiles are compared through the CLI envelope, which is the surface a
	// fresh agent actually reads.
	standard := dataOf(r.json("report", releaseChange, "--kind", "status", "--profile", "default"))
	production := dataOf(r.json("report", releaseChange, "--kind", "status", "--profile", "production"))
	if len(standard) == 0 || len(production) == 0 {
		t.Fatalf("a profile projected no facts: %v %v", standard, production)
	}
	if standard["profile"] == production["profile"] ||
		standard["policy_digest"] == production["policy_digest"] {
		t.Fatal("the production profile projects the same policy as the default")
	}
	// The experimental profile cannot weaken the default: the default keeps its
	// own assurance and rules whatever the production projection reports.
	if standard["assurance"] != core.AttemptAssurance {
		t.Fatalf("the default profile changed its assurance: %v", standard)
	}
}

// releaseResumeJourney is the cold-resume proof: nine facts, each derived from
// repository bytes and canonical projections only, and each cross-checked
// against a second projection of the same truth.
func releaseResumeJourney(t *testing.T) {
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()

	status := r.json("status", releaseChange)
	rendered := r.must("status", releaseChange)
	owner, err := corepath.New(r.root)
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := generate.Render()
	if err != nil {
		t.Fatal(err)
	}
	authored := plan.ParseChange(owner, releaseChange)

	// 1. selected root.
	if got := status["root"].(map[string]any)["path"]; got != r.root {
		t.Fatalf("root = %v, want %s", got, r.root)
	}
	// 2. current change and lifecycle.
	if got := status["subject"].(map[string]any)["change"]; got != releaseChange {
		t.Fatalf("change = %v", got)
	}
	if dataOf(status)["stage"] != "approved" {
		t.Fatalf("lifecycle = %v", dataOf(status)["stage"])
	}
	// 3. accepted specs versus proposed deltas.
	if _, err := os.Stat(filepath.Join(r.root, ".specd", "specs", "sample")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("accepted specs exist before any sync: %v", err)
	}
	if len(authored.Deltas) != 1 || authored.Deltas[0].Capability != "sample" {
		t.Fatalf("proposed deltas = %#v", authored.Deltas)
	}
	// 4. approval freshness.
	if dataOf(status)["approval_current"] != true || !strings.Contains(rendered, "approval_current: true") {
		t.Fatalf("approval freshness disagrees between projections:\n%s", rendered)
	}
	// 5. the full ready frontier.
	frontier := stringsOf(status, "frontier")
	if !slices.Equal(frontier, stringsOf(r.json("next", releaseChange), "frontier")) {
		t.Fatalf("status frontier %v disagrees with next", frontier)
	}
	if len(frontier) == 0 {
		t.Fatal("no ready frontier is derivable")
	}
	// 6. one task context and its declared files.
	manifest := r.json("context", releaseChange, frontier[0])
	declared := stringsOf(manifest, "allowed_write_paths")
	if !slices.Equal(declared, []string{"sample.go"}) {
		t.Fatalf("declared files = %v", declared)
	}
	// 7. the verification command, from the authored bytes the harness parses.
	task, found := releaseAuthoredTask(authored, frontier[0])
	if !found || task.Verify == "" {
		t.Fatalf("no verification command for %s", frontier[0])
	}
	if !slices.Equal(task.Files, declared) {
		t.Fatalf("authored files %v disagree with the projected scope %v", task.Files, declared)
	}
	// 8. the next legal action, identical in both projections.
	next := releaseOneNextAction(t, manifest)
	if !strings.Contains(rendered, "next: ") {
		t.Fatalf("the rendered projection names no next action:\n%s", rendered)
	}
	if instruction, _ := next["instruction"].(string); instruction == "" {
		t.Fatalf("next action carries no instruction: %v", next)
	}
	// 9. the owners of state, evidence, and accepted behavior.
	for _, owned := range []string{"state.json", "evidence.jsonl", "`.specd/`"} {
		if !strings.Contains(guidance.Body, owned) {
			t.Fatalf("generated guidance does not name the owner of %s", owned)
		}
	}
	if !strings.Contains(guidance.Body, "harness owns") {
		t.Fatalf("generated guidance does not name the harness as owner:\n%s", guidance.Body)
	}
}

// ------------------------------------------------------------------ helpers

// release is one isolated project under qualification.
type release struct {
	t    *testing.T
	root string
}

func newRelease(t *testing.T, project, planFiles map[string]string) release {
	t.Helper()
	root := tempRoot(t)
	copyTree(t, filepath.Join("testdata", "release", "fresh-project", "project"), root)
	releaseWrite(t, root, project)
	git(t, root,
		[]string{"init"},
		[]string{"config", "user.email", releaseHuman},
		[]string{"config", "user.name", "Human"},
		[]string{"add", "."},
		[]string{"commit", "-m", "baseline", "--no-gpg-sign"},
	)
	r := release{t: t, root: root}
	r.must("init")
	// Adoption installs the guidance a fresh agent resumes from: journey 14
	// reads this file, so journey 01 proves adoption produces it.
	installed, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("init did not install the agent guidance: %v", err)
	}
	if !strings.Contains(string(installed), "<!-- specd:begin schema=") {
		t.Fatalf("installed guidance carries no managed region:\n%s", installed)
	}
	r.must("new", releaseChange, "--capability", "sample")
	copyTree(t, filepath.Join("testdata", "release", "fresh-project", "plan"),
		filepath.Join(root, ".specd", "changes", releaseChange))
	releaseWrite(t, root, planFiles)
	return r
}

func (r release) agent(args ...string) (int, string) {
	r.t.Helper()
	code, stdout, stderr := runCLI(r.t, cmd.RouteAgent, append(args, "--root", r.root)...)
	return code, stdout + stderr
}

func (r release) must(args ...string) string {
	r.t.Helper()
	code, out := r.agent(args...)
	if code != 0 {
		r.t.Fatalf("specd %v exited %d: %s", args, code, out)
	}
	return out
}

func (r release) jsonAny(args ...string) (int, map[string]any) {
	r.t.Helper()
	code, stdout, stderr := runCLI(r.t, cmd.RouteAgent, append(append(args, "--root", r.root), "--json")...)
	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		r.t.Fatalf("specd %v: %v: %s%s", args, err, stdout, stderr)
	}
	return code, document
}

func (r release) json(args ...string) map[string]any {
	r.t.Helper()
	code, document := r.jsonAny(args...)
	if code != 0 {
		r.t.Fatalf("specd %v exited %d: %v", args, code, document)
	}
	return document
}

// refuses asserts one fail-closed refusal: a non-zero exit, one of the named
// refusal codes, and exactly one legal next action.
func (r release) refuses(args []string, codes ...string) map[string]any {
	r.t.Helper()
	code, document := r.jsonAny(args...)
	if code == 0 {
		r.t.Fatalf("specd %v succeeded, want refusal %v", args, codes)
	}
	diagnostics := releaseDiagnostics(r.t, document)
	if len(diagnostics) == 0 {
		r.t.Fatalf("specd %v refused without a diagnostic: %v", args, document)
	}
	observed, _ := diagnostics[0]["code"].(string)
	if !slices.Contains(codes, observed) {
		r.t.Fatalf("specd %v refused %q, want one of %v", args, observed, codes)
	}
	releaseOneNextAction(r.t, document)
	return document
}

func (r release) humanRun(args ...string) (int, string) {
	r.t.Helper()
	code, stdout, stderr := runCLI(r.t, cmd.RouteHumanTerminal, append(args, "--root", r.root)...)
	return code, stdout + stderr
}

func (r release) approve() { r.t.Helper(); r.approveChange(releaseChange) }

// approveChange passes the gate the only way it can be passed: through the
// human route with a named approver and reason.
func (r release) approveChange(change string) {
	r.t.Helper()
	code, out := r.humanRun("approve", change, "--approver", releaseHuman, "--reason", "reviewed the plan")
	if code != 0 {
		r.t.Fatalf("human approval exited %d: %s", code, out)
	}
	git(r.t, r.root, []string{"add", "-A"}, []string{"commit", "-m", "plan", "--no-gpg-sign"})
}

func (r release) sync() map[string]any { r.t.Helper(); return r.syncChange(releaseChange) }

func (r release) syncChange(change string) map[string]any {
	r.t.Helper()
	code, stdout, stderr := runCLI(r.t, cmd.RouteHumanTerminal, "sync", change,
		"--approver", releaseHuman, "--reason", "reviewed the reconciliation", "--root", r.root, "--json")
	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		r.t.Fatalf("sync: %v: %s%s", err, stdout, stderr)
	}
	if code != 0 {
		r.t.Fatalf("sync exited %d: %v", code, document)
	}
	return document
}

// runTask drives one task from context to completion, editing exactly the file
// it declared.
func (r release) runTask(change, task, file, body string) {
	r.t.Helper()
	manifest := r.json("context", change, task)
	started := r.json("start", change, task, "--revision", revisionOf(r.t, manifest))
	releaseWrite(r.t, r.root, map[string]string{file: body})
	verified := r.json("verify", change, task, dataString(r.t, started, "attempt"))
	if dataOf(verified)["passed"] != true {
		r.t.Fatalf("task %s did not verify: %v", task, dataOf(verified))
	}
	r.json("complete", change, task, "--revision", revisionOf(r.t, verified))
	// The next attempt needs a clean tracked baseline, so the completed work is
	// committed exactly as a caller would commit it.
	git(r.t, r.root, []string{"add", "-A"}, []string{"commit", "-m", "task " + task, "--no-gpg-sign"})
}

// releaseFact reads one named fact out of a canonical report projection.
func releaseFact(t *testing.T, result cmd.ReportResult, name string) string {
	t.Helper()
	for _, fact := range result.Facts {
		if fact.Field == name {
			return fact.Value
		}
	}
	t.Fatalf("report %q carries no %q fact: %#v", result.Kind, name, result.Facts)
	return ""
}

func releaseWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func releaseTaskTable(rows ...string) string {
	return "| id | role | files | depends-on | refs | verify | acceptance |\n|---|---|---|---|---|---|---|\n" +
		strings.Join(rows, "\n") + "\n"
}

func releaseTaskRow(id, file, dependsOn string) string {
	return "| " + id + " | builder | " + file + " | " + dependsOn +
		" | sample/Requirement: Current greeting | `go test . -run '^TestSampleLoop$' -count=1` | " +
		"the " + id + " file exists and its test passes |"
}

func releaseDiagnostics(t *testing.T, document map[string]any) []map[string]any {
	t.Helper()
	raw, _ := document["diagnostics"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("diagnostic is not an object: %v", item)
		}
		out = append(out, entry)
	}
	return out
}

// releaseOneNextAction asserts the document offers exactly one legal next
// action and returns it.
func releaseOneNextAction(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	next, ok := document["next"].(map[string]any)
	if !ok {
		t.Fatalf("document names no next action: %v", document)
	}
	if instruction, _ := next["instruction"].(string); strings.TrimSpace(instruction) == "" {
		t.Fatalf("next action carries no instruction: %v", next)
	}
	return next
}

// releaseRecords counts the valid records in one managed ledger, so a torn tail
// is never counted as a usable record.
func releaseRecords(t *testing.T, root, ledger string) int {
	t.Helper()
	family := record.FamilyEvidence
	if ledger == "history.jsonl" {
		family = record.FamilyHistory
	}
	records, diagnostics, err := record.Replay(filepath.Join(root, ".specd", ledger), family)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("%s has an incomplete tail: %v", ledger, diagnostics)
	}
	return len(records)
}

func releaseAuthoredTask(authored plan.Change, id string) (plan.Task, bool) {
	for _, task := range authored.Tasks.Tasks {
		if task.ID == id {
			return task, true
		}
	}
	return plan.Task{}, false
}

// releaseStdin gives the human gate a non-terminal stream, so approval stays on
// the non-interactive path that demands an explicit approver and reason.
func releaseStdin(t *testing.T) {
	t.Helper()
	original := os.Stdin
	os.Stdin = regularStdin(t)
	t.Cleanup(func() { os.Stdin = original })
}
