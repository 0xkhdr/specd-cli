package transaction

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
)

var clock = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

func newRoot(t *testing.T) *corepath.Owner {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".specd", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func seed(t *testing.T, owner *corepath.Owner, relative, content string) string {
	t.Helper()
	target := filepath.Join(owner.Managed(), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return hash([]byte(content))
}

func read(t *testing.T, owner *corepath.Owner, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(owner.Managed(), filepath.FromSlash(relative)))
	if err != nil {
		return ""
	}
	return string(raw)
}

func request(outputs ...Write) Request {
	return Request{Operation: "sync", Change: "safe-create", Revision: 7, Now: clock, Outputs: outputs}
}

func residue(t *testing.T, owner *corepath.Owner) []string {
	t.Helper()
	entries, err := os.ReadDir(owner.Managed())
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			found = append(found, entry.Name())
		}
	}
	return found
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected refusal %s, got success", code)
	}
	var refusal *failure.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("expected refusal %s, got %v", code, err)
	}
	if refusal.Code != code {
		t.Fatalf("expected code %s, got %s (%s)", code, refusal.Code, refusal.Reason)
	}
	if strings.TrimSpace(refusal.Next) == "" {
		t.Fatal("every refusal names exactly one next action")
	}
	if strings.Contains(strings.ToLower(refusal.Next), "edit") {
		t.Fatalf("refusal advises a manual edit: %s", refusal.Next)
	}
}

func TestCommitWritesEveryOutputOnce(t *testing.T) {
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old accounts\n")

	result, err := Commit(owner, request(
		Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new accounts\n")},
		Write{Path: "specs/billing/spec.md", Bytes: []byte("new billing\n")},
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.NoOp || result.ID == "" {
		t.Fatalf("expected a committed transaction, got %+v", result)
	}
	if got := read(t, owner, "specs/accounts/spec.md"); got != "new accounts\n" {
		t.Fatalf("accounts = %q", got)
	}
	if got := read(t, owner, "specs/billing/spec.md"); got != "new billing\n" {
		t.Fatalf("billing = %q", got)
	}
	if left := residue(t, owner); len(left) != 0 {
		t.Fatalf("transaction residue remains: %v", left)
	}
}

func TestCommitIsIdempotent(t *testing.T) {
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := request(Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")})

	if _, err := Commit(owner, plan); err != nil {
		t.Fatal(err)
	}
	second, err := Commit(owner, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !second.NoOp {
		t.Fatal("re-running the same plan must be a no-op")
	}
	if got := read(t, owner, "specs/accounts/spec.md"); got != "new\n" {
		t.Fatalf("accounts = %q", got)
	}
	if left := residue(t, owner); len(left) != 0 {
		t.Fatalf("a no-op must leave no transaction: %v", left)
	}
}

func TestCommitRefusesSourceDrift(t *testing.T) {
	owner := newRoot(t)
	seed(t, owner, "specs/accounts/spec.md", "current\n")

	_, err := Commit(owner, request(
		Write{Path: "specs/accounts/spec.md", Before: hash([]byte("planned-from\n")), Bytes: []byte("new\n")},
	))
	requireCode(t, err, CodeDrift)
	if got := read(t, owner, "specs/accounts/spec.md"); got != "current\n" {
		t.Fatalf("drift must preserve originals, got %q", got)
	}
}

func TestCommitRefusesUnsafeOutputs(t *testing.T) {
	owner := newRoot(t)
	cases := map[string]Write{
		"escape":    {Path: "../escape.md", Bytes: []byte("x")},
		"absolute":  {Path: "/etc/passwd", Bytes: []byte("x")},
		"reserved":  {Path: ".root.lock", Bytes: []byte("x")},
		"backslash": {Path: `specs\accounts\spec.md`, Bytes: []byte("x")},
		"nil bytes": {Path: "specs/accounts/spec.md"},
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Commit(owner, request(write))
			requireCode(t, err, CodeInvalid)
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		_, err := Commit(owner, request(
			Write{Path: "specs/a/spec.md", Bytes: []byte("x")},
			Write{Path: "specs/a/spec.md", Bytes: []byte("y")},
		))
		requireCode(t, err, CodeInvalid)
	})
	t.Run("empty plan", func(t *testing.T) {
		_, err := Commit(owner, request())
		requireCode(t, err, CodeInvalid)
	})
}

func TestCommitConcurrentAttemptsAgree(t *testing.T) {
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := request(Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")})

	var group sync.WaitGroup
	errs := make([]error, 8)
	for index := range errs {
		group.Add(1)
		go func(slot int) {
			defer group.Done()
			_, errs[slot] = Commit(owner, plan)
		}(index)
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, owner, "specs/accounts/spec.md"); got != "new\n" {
		t.Fatalf("accounts = %q", got)
	}
	if left := residue(t, owner); len(left) != 0 {
		t.Fatalf("transaction residue remains: %v", left)
	}
}

// interrupt commits with a hook that fails at one boundary.
func interrupt(t *testing.T, owner *corepath.Owner, plan Request, boundary string) {
	t.Helper()
	plan.Hook = func(step string) error {
		if step == boundary {
			return errors.New("injected interruption at " + boundary)
		}
		return nil
	}
	if _, err := Commit(owner, plan); err == nil {
		t.Fatalf("expected interruption at %s", boundary)
	}
}

func TestRecoverRollsBackUncommittedStaging(t *testing.T) {
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := request(Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")})

	interrupt(t, owner, plan, "after-stage")
	if got := read(t, owner, "specs/accounts/spec.md"); got != "old\n" {
		t.Fatalf("uncommitted staging must not change data, got %q", got)
	}
	pending, err := Inspect(owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Action != ActionRollback {
		t.Fatalf("expected one rollback, got %+v", pending)
	}
	if _, err := Recover(owner, clock); err != nil {
		t.Fatal(err)
	}
	if got := read(t, owner, "specs/accounts/spec.md"); got != "old\n" {
		t.Fatalf("rollback must keep old bytes, got %q", got)
	}
	if left := residue(t, owner); len(left) != 0 {
		t.Fatalf("rollback must clear staging: %v", left)
	}
}

func TestRecoverRollsForwardCommittedTransaction(t *testing.T) {
	for _, boundary := range []string{
		"after-manifest",
		"before-replace:specs/billing/spec.md",
		"before-cleanup",
	} {
		t.Run(boundary, func(t *testing.T) {
			owner := newRoot(t)
			before := seed(t, owner, "specs/accounts/spec.md", "old\n")
			plan := request(
				Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new accounts\n")},
				Write{Path: "specs/billing/spec.md", Bytes: []byte("new billing\n")},
			)
			interrupt(t, owner, plan, boundary)

			pending, err := Inspect(owner, clock)
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].Action != ActionRollForward {
				t.Fatalf("expected one roll-forward, got %+v", pending)
			}
			// Recovery is repeatable and the same identity yields the same action.
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := Recover(owner, clock); err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
			}
			if got := read(t, owner, "specs/accounts/spec.md"); got != "new accounts\n" {
				t.Fatalf("accounts = %q", got)
			}
			if got := read(t, owner, "specs/billing/spec.md"); got != "new billing\n" {
				t.Fatalf("billing = %q", got)
			}
			if left := residue(t, owner); len(left) != 0 {
				t.Fatalf("transaction residue remains: %v", left)
			}
		})
	}
}

func TestRecoverRunsBeforeTheNextCommit(t *testing.T) {
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := request(Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")})
	interrupt(t, owner, plan, "after-manifest")

	// The retry is the same call: it rolls the pending transaction forward and
	// then observes its own plan already applied.
	result, err := Commit(owner, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoOp {
		t.Fatal("retry after roll-forward must be a no-op")
	}
	if got := read(t, owner, "specs/accounts/spec.md"); got != "new\n" {
		t.Fatalf("accounts = %q", got)
	}
}

// journal returns the single pending manifest path.
func journal(t *testing.T, owner *corepath.Owner) string {
	t.Helper()
	left := residue(t, owner)
	for _, name := range left {
		if strings.HasSuffix(name, manifestSuffix) {
			return filepath.Join(owner.Managed(), name)
		}
	}
	t.Fatalf("no pending manifest in %v", left)
	return ""
}

func pendingRoot(t *testing.T) (*corepath.Owner, Request) {
	t.Helper()
	owner := newRoot(t)
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := request(Write{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")})
	interrupt(t, owner, plan, "after-manifest")
	return owner, plan
}

func TestRecoverFailsClosedOnBadManifests(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		if err := os.WriteFile(journal(t, owner), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Recover(owner, clock)
		requireCode(t, err, CodeInvalid)
		if got := read(t, owner, "specs/accounts/spec.md"); got != "old\n" {
			t.Fatalf("a malformed manifest must not change data, got %q", got)
		}
	})

	t.Run("identity mismatch", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		path := journal(t, owner)
		var manifest Manifest
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Change = "other-change"
		edited, _ := json.Marshal(manifest)
		if err := os.WriteFile(path, edited, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = Recover(owner, clock)
		requireCode(t, err, CodeInvalid)
	})

	t.Run("future timestamp", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		_, err := Recover(owner, clock.Add(-time.Hour))
		requireCode(t, err, CodeInvalid)
		if got := read(t, owner, "specs/accounts/spec.md"); got != "old\n" {
			t.Fatalf("a future manifest must not change data, got %q", got)
		}
	})

	t.Run("unsupported schema", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		path := journal(t, owner)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.SchemaVersion = 99
		edited, _ := json.Marshal(manifest)
		if err := os.WriteFile(path, edited, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = Recover(owner, clock)
		requireCode(t, err, CodeInvalid)
	})

	t.Run("missing staged bytes", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		for _, name := range residue(t, owner) {
			if !strings.HasSuffix(name, manifestSuffix) {
				if err := os.RemoveAll(filepath.Join(owner.Managed(), name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		_, err := Recover(owner, clock)
		requireCode(t, err, CodeAmbiguous)
		if got := read(t, owner, "specs/accounts/spec.md"); got != "old\n" {
			t.Fatalf("missing staging must not change data, got %q", got)
		}
	})

	t.Run("ambiguous target", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		target := filepath.Join(owner.Managed(), "specs", "accounts", "spec.md")
		if err := os.WriteFile(target, []byte("edited by hand\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Recover(owner, clock)
		requireCode(t, err, CodeAmbiguous)
		if got := read(t, owner, "specs/accounts/spec.md"); got != "edited by hand\n" {
			t.Fatalf("ambiguous recovery must not overwrite, got %q", got)
		}
	})

	t.Run("renamed manifest", func(t *testing.T) {
		owner, _ := pendingRoot(t)
		path := journal(t, owner)
		if err := os.Rename(path, filepath.Join(owner.Managed(), prefix+"renamed"+manifestSuffix)); err != nil {
			t.Fatal(err)
		}
		_, err := Recover(owner, clock)
		requireCode(t, err, CodeInvalid)
	})
}

func moveRequest(t *testing.T, owner *corepath.Owner) Request {
	t.Helper()
	seed(t, owner, "changes/safe-create/proposal.md", "proposal\n")
	before := seed(t, owner, "changes/safe-create/state.json", "old state\n")
	return Request{
		Operation: "archive", Change: "safe-create", Revision: 3, Now: clock,
		Outputs: []Write{{
			Path: "changes/safe-create/state.json", Before: before, Bytes: []byte("new state\n"),
		}},
		Moves: []Move{{From: "changes/safe-create", To: "archive/2026-07-30-safe-create"}},
	}
}

func TestCommitMovesAFolderAsOneUnit(t *testing.T) {
	owner := newRoot(t)
	if _, err := Commit(owner, moveRequest(t, owner)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(owner.Managed(), "changes", "safe-create")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the move source survived")
	}
	if got := read(t, owner, "archive/2026-07-30-safe-create/state.json"); got != "new state\n" {
		t.Fatalf("moved state = %q", got)
	}
	if got := read(t, owner, "archive/2026-07-30-safe-create/proposal.md"); got != "proposal\n" {
		t.Fatalf("moved proposal = %q", got)
	}
	if left := residue(t, owner); len(left) != 0 {
		t.Fatalf("transaction residue remains: %v", left)
	}
}

func TestRecoverRollsForwardAnInterruptedMove(t *testing.T) {
	for _, boundary := range []string{
		"after-manifest",
		"before-replace:changes/safe-create/state.json",
		"before-move:changes/safe-create",
	} {
		t.Run(boundary, func(t *testing.T) {
			owner := newRoot(t)
			plan := moveRequest(t, owner)
			interrupt(t, owner, plan, boundary)
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := Recover(owner, clock); err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
			}
			if _, err := os.Stat(filepath.Join(owner.Managed(), "changes", "safe-create")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("recovery left the move source in place")
			}
			// The file output lives inside the moved folder: recovery must
			// find it at its new home rather than read it as lost.
			if got := read(t, owner, "archive/2026-07-30-safe-create/state.json"); got != "new state\n" {
				t.Fatalf("moved state = %q", got)
			}
			if left := residue(t, owner); len(left) != 0 {
				t.Fatalf("transaction residue remains: %v", left)
			}
		})
	}
}

func TestCommitRefusesOverlappingMoves(t *testing.T) {
	owner := newRoot(t)
	plan := moveRequest(t, owner)
	plan.Moves = []Move{{From: "changes", To: "changes/safe-create"}}
	_, err := Commit(owner, plan)
	requireCode(t, err, CodeInvalid)
}

func TestRecoverAppendsTheBoundRecordExactlyOnce(t *testing.T) {
	owner := newRoot(t)
	if err := os.WriteFile(owner.History(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := record.NewCompletionPayload(record.CompletionPayload{
		Change: "safe-create", TaskID: "T1",
		AttemptID: strings.Repeat("a", 64), EvidenceID: strings.Repeat("b", 64),
		Actor: "agent:builder", ActivityFrom: "in_progress", ActivityTo: "completed",
		RevisionBefore: 3, RevisionAfter: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(payload)
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindCompletion, Change: "safe-create",
		ExpectedRevision: record.Revision(3), ResultingRevision: record.Revision(4),
		Timestamp: clock.Format(time.RFC3339Nano), Actor: "agent:builder", Payload: encoded,
	})
	if err != nil {
		t.Fatal(err)
	}
	before := seed(t, owner, "specs/accounts/spec.md", "old\n")
	plan := Request{
		Operation: "sync", Change: "safe-create", Revision: 3, Now: clock,
		Outputs: []Write{{Path: "specs/accounts/spec.md", Before: before, Bytes: []byte("new\n")}},
		Record:  &item,
	}
	// Interrupt after the outputs are committed but before the record lands,
	// then prove recovery appends the one missing record and no duplicate.
	interrupt(t, owner, plan, "before-cleanup")
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := Recover(owner, clock); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	items, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("history: %v %v", err, diagnostics)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("history = %+v", items)
	}
}

func TestRecoverIgnoresAnUntouchedRoot(t *testing.T) {
	owner := newRoot(t)
	pending, err := Recover(owner, clock)
	if err != nil || len(pending) != 0 {
		t.Fatalf("clean root: %v %+v", err, pending)
	}
}
