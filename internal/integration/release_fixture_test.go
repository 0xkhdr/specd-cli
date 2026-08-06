package integration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

type releaseFixture struct {
	Schema    int    `json:"schema"`
	Invariant string `json:"invariant"`
	Scenario  string `json:"scenario"`
	Refusal   string `json:"refusal,omitempty"`
	Next      string `json:"next,omitempty"`
}

func TestReleaseFixtureContract(t *testing.T) {
	releaseStdin(t)
	entries, err := os.ReadDir(filepath.Join("testdata", "release"))
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	consumed := 0
	for _, entry := range entries {
		if !entry.IsDir() || (!strings.HasPrefix(entry.Name(), "good_") && !strings.HasPrefix(entry.Name(), "bad_")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("testdata", "release", entry.Name(), "case.json"))
		if err != nil {
			t.Fatalf("fixture %s is unreadable: %v", entry.Name(), err)
		}
		var fixture releaseFixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("fixture %s is malformed: %v", entry.Name(), err)
		}
		if fixture.Schema != 1 || fixture.Scenario == "" {
			t.Fatalf("fixture %s is incomplete: %#v", entry.Name(), fixture)
		}
		if strings.HasPrefix(entry.Name(), "bad_") {
			if fixture.Invariant == "" || fixture.Refusal == "" || fixture.Next == "" {
				t.Fatalf("adversarial fixture %s is incomplete: %#v", entry.Name(), fixture)
			}
			covered[fixture.Invariant] = true
		}
		consumed++
		t.Run(entry.Name(), func(t *testing.T) {
			code, next := runReleaseFixture(t, fixture.Scenario)
			if code != fixture.Refusal || next != fixture.Next {
				t.Fatalf("observed refusal/next = %q / %q, want %q / %q", code, next, fixture.Refusal, fixture.Next)
			}
		})
	}
	if consumed == 0 {
		t.Fatal("no named release fixtures were consumed")
	}
	for _, invariant := range protectedInvariants {
		if !covered[invariant] {
			t.Errorf("protected invariant %q has no adversarial fixture", invariant)
		}
	}
}

func runReleaseFixture(t *testing.T, scenario string) (string, string) {
	t.Helper()
	switch scenario {
	case "good_base_loop":
		releaseFreshProject(t)
		return "", ""
	case "approval_self_granted":
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "approve", releaseChange)
		return fixtureRefusal(t, document)
	case "authority_forged_actor":
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		_, document := fixtureJSON(t, r, cmd.RouteHumanTerminal, "approve", releaseChange,
			"--approver", "forged@example.com", "--reason", "forged")
		return fixtureRefusal(t, document)
	case "scope_undeclared_write":
		r, attempt := startedReleaseFixture(t)
		releaseWrite(t, r.root, map[string]string{"sample.go": afterSample, "other.go": "package sample\n"})
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "verify", releaseChange, releaseTask, attempt)
		return fixtureRefusal(t, document)
	case "evidence_stale_head":
		r, attempt := startedReleaseFixture(t)
		releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
		verified := r.json("verify", releaseChange, releaseTask, attempt)
		git(t, r.root, []string{"add", "-A"}, []string{"commit", "-m", "moved", "--no-gpg-sign"})
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "complete", releaseChange, releaseTask,
			"--revision", revisionOf(t, verified))
		return fixtureRefusal(t, document)
	case "evidence_not_applicable":
		r, _ := startedReleaseFixture(t)
		releaseWrite(t, r.root, map[string]string{"sample.go": afterSample})
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "complete", releaseChange, releaseTask,
			"--revision", revisionOf(t, r.json("status", releaseChange)))
		return fixtureRefusal(t, document)
	case "staleness_revision_moved":
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		r.approve()
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "start", releaseChange, releaseTask, "--revision", "999")
		return fixtureRefusal(t, document)
	case "atomicity_torn_write":
		r := newRelease(t, nil, nil)
		r.must("check", releaseChange)
		r.approve()
		r.runTask(releaseChange, releaseTask, "sample.go", afterSample)
		_, err := core.Sync(r.root, releaseChange, core.SyncOptions{
			GitEmail: releaseHuman, ClaimedApprover: releaseHuman, Reason: "fixture",
			Route: core.ApprovalRouteHumanTerminal, Now: time.Now(),
			Hook: func(step string) error {
				if strings.HasPrefix(step, "before-replace:") && strings.HasSuffix(step, "state.json") {
					return errors.New("fixture interruption")
				}
				return nil
			},
		})
		var refusal *failure.Refusal
		if !errors.As(err, &refusal) {
			t.Fatalf("interrupted sync returned %v", err)
		}
		return refusal.Code, refusal.Next
	case "validation_malformed_tasks":
		r := newRelease(t, nil, map[string]string{
			".specd/changes/" + releaseChange + "/tasks.md": "not a task table\n",
		})
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "check", releaseChange)
		return fixtureRefusal(t, document)
	case "failclosed_future_schema":
		r := newRelease(t, nil, nil)
		releaseWrite(t, r.root, map[string]string{
			".specd/changes/" + releaseChange + "/state.json": `{"schemaVersion":99,"change":"release-sample","stage":"planning","condition":"active","revision":1,"approvals":[],"tasks":{},"createdBy":"x","lastTransition":"x"}`,
		})
		_, document := fixtureJSON(t, r, cmd.RouteAgent, "status", releaseChange)
		return fixtureRefusal(t, document)
	default:
		t.Fatalf("unknown fixture scenario %q", scenario)
		return "", ""
	}
}

func startedReleaseFixture(t *testing.T) (release, string) {
	t.Helper()
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	manifest := r.json("context", releaseChange, releaseTask)
	started := r.json("start", releaseChange, releaseTask, "--revision", revisionOf(t, manifest))
	return r, dataString(t, started, "attempt")
}

func fixtureJSON(t *testing.T, r release, route cmd.Route, args ...string) (int, map[string]any) {
	t.Helper()
	code, stdout, stderr := runCLI(t, route, append(append(slices.Clone(args), "--root", r.root), "--json")...)
	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("specd %v: %v: %s%s", args, err, stdout, stderr)
	}
	if code == 0 {
		t.Fatalf("specd %v succeeded, want refusal", args)
	}
	return code, document
}

func fixtureRefusal(t *testing.T, document map[string]any) (string, string) {
	t.Helper()
	diagnostics := releaseDiagnostics(t, document)
	if len(diagnostics) == 0 {
		t.Fatalf("refusal has no diagnostic: %v", document)
	}
	code, _ := diagnostics[0]["code"].(string)
	next := releaseOneNextAction(t, document)
	instruction, _ := next["instruction"].(string)
	return code, instruction
}
