// Concurrent callers are the one thing the base loop is exercised by rather
// than argued about. The root and change locks are meant to serialize managed
// writes, and lock_test.go proves the primitive in isolation, but a primitive
// is not a guarantee: the in-process mutex in internal/core/lock would satisfy
// an in-process test on its own and never touch the file lock. So every racing
// caller here is a real specd process, contending for one root exactly as two
// terminals would.
package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/cli"
	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core/record"
)

// concurrentCallers is how many processes contend. It is small on purpose: the
// claim is that contention is decided, not that the harness scales.
const concurrentCallers = 6

// contestedRefusals are the codes a caller that loses a contested transition
// may report. Anything else is a defect, not a variant: a loser must fail
// closed on a named refusal, never on a partial write or a generic error.
var contestedRefusals = []string{"stale_revision", "lock_busy", "attempt_exists", "task_not_ready"}

// TestConcurrentCallersOneRoot answers limitation 2 of release-decision.md for
// contention: two or more callers against one root either serialize or refuse,
// and never lose a write.
func TestConcurrentCallersOneRoot(t *testing.T) {
	releaseStdin(t)
	binary := buildSpecd(t)
	t.Run("a contested transition elects exactly one caller", func(t *testing.T) {
		concurrentStartJourney(t, binary)
	})
	t.Run("independent writes serialize without loss", func(t *testing.T) {
		concurrentNewJourney(t, binary)
	})
}

// concurrentStartJourney races the same task transition from every caller at
// one revision. Exactly one may open the attempt; every other caller must
// refuse with a named code and one legal next action, and the ledger must
// record the one attempt that was elected rather than the ones that were not.
func concurrentStartJourney(t *testing.T, binary string) {
	t.Helper()
	r := newRelease(t, nil, nil)
	r.must("check", releaseChange)
	r.approve()
	revision := revisionOf(t, r.json("next", releaseChange))

	calls := make([][]string, concurrentCallers)
	for i := range calls {
		calls[i] = []string{"start", releaseChange, releaseTask, "--revision", revision}
	}

	elected := 0
	for i, result := range race(t, binary, r.root, calls) {
		if result.code == 0 {
			elected++
			continue
		}
		diagnostics := releaseDiagnostics(t, result.document)
		if len(diagnostics) == 0 {
			t.Fatalf("caller %d refused without a diagnostic: %v", i, result.document)
		}
		code, _ := diagnostics[0]["code"].(string)
		if !slices.Contains(contestedRefusals, code) {
			t.Fatalf("caller %d refused %q, want one of %v", i, code, contestedRefusals)
		}
		releaseOneNextAction(t, result.document)
	}
	if elected != 1 {
		t.Fatalf("%d of %d callers opened the attempt, want exactly 1", elected, concurrentCallers)
	}

	attempts := 0
	for _, entry := range concurrentHistory(t, r.root) {
		if entry.Kind == record.KindAttempt {
			attempts++
		}
	}
	if attempts != 1 {
		t.Fatalf("history records %d attempts, want exactly 1", attempts)
	}
}

// concurrentNewJourney races appends that have no reason to conflict. They
// share the root's history ledger, so the failure this rules out is a lost or
// torn append: every caller must succeed and every append must survive.
func concurrentNewJourney(t *testing.T, binary string) {
	t.Helper()
	r := newRelease(t, nil, nil)
	before := len(concurrentHistory(t, r.root))

	calls := make([][]string, concurrentCallers)
	names := make([]string, concurrentCallers)
	for i := range calls {
		names[i] = fmt.Sprintf("race-%02d", i)
		calls[i] = []string{"new", names[i], "--capability", "sample"}
	}

	for i, result := range race(t, binary, r.root, calls) {
		if result.code != 0 {
			t.Fatalf("caller %d exited %d under contention: %v", i, result.code, result.document)
		}
	}

	// concurrentHistory refuses a torn tail, so a clean replay of the expected
	// length is the whole no-lost-write claim.
	history := concurrentHistory(t, r.root)
	if grew := len(history) - before; grew != concurrentCallers {
		t.Fatalf("history grew by %d records, want %d", grew, concurrentCallers)
	}

	identities := make(map[string]bool, len(history))
	created := make(map[string]int, concurrentCallers)
	for _, entry := range history {
		if identities[entry.ID] {
			t.Fatalf("two records share identity %s", entry.ID)
		}
		identities[entry.ID] = true
		if entry.Kind == record.KindCreated {
			created[entry.Change]++
		}
	}
	for _, name := range names {
		if created[name] != 1 {
			t.Fatalf("change %s was created %d times, want exactly 1", name, created[name])
		}
		if _, err := os.Stat(filepath.Join(r.root, ".specd", "changes", name, "state.json")); err != nil {
			t.Fatalf("change %s has no state: %v", name, err)
		}
	}
}

// ------------------------------------------------------------------- harness

// concurrentCall is one racing caller's observed result.
type concurrentCall struct {
	code     int
	document map[string]any
}

// race runs every call as its own process against one root and releases them
// from one barrier, so they contend rather than queue. Each is spawned on the
// agent route explicitly: inheriting whatever route the environment happens to
// carry would make the contention a different test on a different host.
func race(t *testing.T, binary, root string, calls [][]string) []concurrentCall {
	t.Helper()
	release := make(chan struct{})
	results := make([]concurrentCall, len(calls))
	failures := make([]error, len(calls))

	var running sync.WaitGroup
	for i, args := range calls {
		running.Add(1)
		go func() {
			defer running.Done()
			process := exec.Command(binary, slices.Concat(args, []string{"--root", root, "--json"})...)
			process.Env = append(os.Environ(), cli.RouteVariable+"="+string(cmd.RouteAgent))
			var stdout, stderr bytes.Buffer
			process.Stdout, process.Stderr = &stdout, &stderr

			<-release
			err := process.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				failures[i] = fmt.Errorf("run %v: %w", args, err)
				return
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				failures[i] = fmt.Errorf("%v emitted no envelope: %w: %s%s", args, err, &stdout, &stderr)
				return
			}
			results[i] = concurrentCall{code: process.ProcessState.ExitCode(), document: document}
		}()
	}
	close(release)
	running.Wait()

	// Reported from the test goroutine: a subprocess failure is the harness
	// failing, not the harness under test refusing.
	if err := errors.Join(failures...); err != nil {
		t.Fatalf("racing callers: %v", err)
	}
	return results
}

// concurrentHistory replays the root's history ledger and refuses an incomplete
// tail, so a record torn by a concurrent append can never be read as a valid
// one.
func concurrentHistory(t *testing.T, root string) []record.Record {
	t.Helper()
	records, diagnostics, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("history.jsonl has an incomplete tail: %v", diagnostics)
	}
	return records
}

// buildSpecd builds the binary the racing callers run. It is built rather than
// looked up so the contention is proven against this tree, not against whatever
// specd the host has installed.
func buildSpecd(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "specd")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "github.com/0xkhdr/specd-cli/cmd/specd")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build specd: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return binary
}
