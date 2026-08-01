package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
)

// archiveClock is deliberately a local time whose UTC date differs, so the
// archive prefix proves it uses the injected local calendar date.
var archiveClock = time.Date(2026, 7, 30, 23, 30, 0, 0, time.FixedZone("local", 2*60*60))

func archiveReadyRoot(t *testing.T) string {
	t.Helper()
	root := syncReadyRoot(t)
	if _, err := Sync(root, "safe-change", syncOptions()); err != nil {
		t.Fatal(err)
	}
	return root
}

func archiveOptions() ArchiveOptions {
	return ArchiveOptions{Actor: "agent:builder", Now: archiveClock}
}

func changeDir(root string) string {
	return filepath.Join(root, ".specd", "changes", "safe-change")
}

func archiveDir(root string) string {
	return filepath.Join(root, ".specd", "archive", "2026-07-30-safe-change")
}

func TestArchiveMovesTheCompleteChange(t *testing.T) {
	root := archiveReadyRoot(t)
	acceptedBefore, err := os.ReadFile(acceptedPath(root))
	if err != nil {
		t.Fatal(err)
	}

	result, err := Archive(root, "safe-change", archiveOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Target != "archive/2026-07-30-safe-change" || result.Source != "changes/safe-change" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(changeDir(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the active change folder survived the archive")
	}
	for _, name := range []string{"proposal.md", "design.md", "tasks.md", "state.json",
		filepath.Join("specs", "sample", "spec.md")} {
		if _, err := os.Stat(filepath.Join(archiveDir(root), name)); err != nil {
			t.Fatalf("archived change is missing %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(archiveDir(root), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"stage": "archived"`) {
		t.Fatalf("archived state = %s", raw)
	}
	acceptedAfter, err := os.ReadFile(acceptedPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(acceptedAfter) != string(acceptedBefore) {
		t.Fatal("archive rewrote accepted truth")
	}

	records := historyKinds(t, root, record.KindArchived)
	if len(records) != 1 {
		t.Fatalf("expected one archive record, got %d", len(records))
	}
	payload, err := record.DecodeArchivePayload(records[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Approver != "human@example.com" || payload.Actor != "agent:builder" ||
		payload.Target != result.Target || payload.ChangeHash != result.ChangeHash ||
		len(payload.Accepted) != 1 || payload.Accepted[0].Path != "specs/sample/spec.md" {
		t.Fatalf("archive payload = %+v", payload)
	}
}

func TestArchiveHasNoExternalSideEffects(t *testing.T) {
	root := archiveReadyRoot(t)
	evidenceBefore, err := os.ReadFile(filepath.Join(root, ".specd", "evidence.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	head := gitHead(t, root)

	if _, err := Archive(root, "safe-change", archiveOptions()); err != nil {
		t.Fatal(err)
	}
	evidenceAfter, err := os.ReadFile(filepath.Join(root, ".specd", "evidence.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(evidenceAfter) != string(evidenceBefore) {
		t.Fatal("archive mutated evidence")
	}
	if gitHead(t, root) != head {
		t.Fatal("archive touched Git")
	}
	// Task activity is proof, not bookkeeping: archive never marks anything.
	raw, err := os.ReadFile(filepath.Join(archiveDir(root), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"T1": "completed"`) {
		t.Fatalf("archived task activity = %s", raw)
	}
}

func gitHead(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestArchiveRefusesEveryUnsafePrecondition(t *testing.T) {
	t.Run("no actor", func(t *testing.T) {
		root := archiveReadyRoot(t)
		_, err := Archive(root, "safe-change", ArchiveOptions{Now: archiveClock})
		syncRefusal(t, err, "archive_unauthorized")
	})

	t.Run("not reconciled", func(t *testing.T) {
		root := syncReadyRoot(t)
		_, err := Archive(root, "safe-change", archiveOptions())
		syncRefusal(t, err, "archive_lifecycle")
		if _, statErr := os.Stat(changeDir(root)); statErr != nil {
			t.Fatal("a refused archive removed the active change")
		}
	})

	t.Run("target collision", func(t *testing.T) {
		root := archiveReadyRoot(t)
		if err := os.MkdirAll(archiveDir(root), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Archive(root, "safe-change", archiveOptions())
		syncRefusal(t, err, "archive_target_exists")
		if _, statErr := os.Stat(changeDir(root)); statErr != nil {
			t.Fatal("a collision removed the active change")
		}
	})

	t.Run("artifacts edited after sync", func(t *testing.T) {
		root := archiveReadyRoot(t)
		proposal := filepath.Join(changeDir(root), "proposal.md")
		if err := os.WriteFile(proposal, []byte("## Problem\nedited after sync\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Archive(root, "safe-change", archiveOptions())
		syncRefusal(t, err, "archive_approval")
		if _, statErr := os.Stat(changeDir(root)); statErr != nil {
			t.Fatal("a stale approval removed the active change")
		}
	})

	t.Run("accepted truth edited after sync", func(t *testing.T) {
		root := archiveReadyRoot(t)
		if err := os.WriteFile(acceptedPath(root), []byte("# Sample\n\n## Purpose\n\nedited\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Archive(root, "safe-change", archiveOptions())
		syncRefusal(t, err, "archive_unsynced")
		if _, statErr := os.Stat(changeDir(root)); statErr != nil {
			t.Fatal("an unsynced delta removed the active change")
		}
	})

	t.Run("evidence removed after sync", func(t *testing.T) {
		root := archiveReadyRoot(t)
		if err := os.WriteFile(filepath.Join(root, ".specd", "evidence.jsonl"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		// The completion binding still names evidence that no longer exists.
		_, err := Archive(root, "safe-change", archiveOptions())
		if err == nil {
			t.Fatal("archive accepted a change whose evidence vanished")
		}
		if _, statErr := os.Stat(changeDir(root)); statErr != nil {
			t.Fatal("a refused archive removed the active change")
		}
	})

	t.Run("path escape", func(t *testing.T) {
		root := archiveReadyRoot(t)
		_, err := Archive(root, "../escape", archiveOptions())
		if err == nil {
			t.Fatal("archive accepted a traversing change selector")
		}
	})
}

func TestArchiveRecoversAnInterruptedMove(t *testing.T) {
	for _, boundary := range []string{
		"after-manifest", "before-move:changes/safe-change", "before-cleanup",
	} {
		t.Run(boundary, func(t *testing.T) {
			root := archiveReadyRoot(t)
			options := archiveOptions()
			options.Hook = func(step string) error {
				if step == boundary {
					return errors.New("injected interruption")
				}
				return nil
			}
			if _, err := Archive(root, "safe-change", options); err == nil {
				t.Fatalf("expected interruption at %s", boundary)
			}
			// Neither the source nor the target may be half written: exactly
			// one of them holds the change after recovery.
			if err := RecoverTransactions(root, archiveClock); err != nil {
				t.Fatal(err)
			}
			_, sourceErr := os.Stat(changeDir(root))
			_, targetErr := os.Stat(archiveDir(root))
			if sourceErr == nil || targetErr != nil {
				t.Fatalf("recovery left source=%v target=%v", sourceErr, targetErr)
			}
			if _, err := os.Stat(filepath.Join(archiveDir(root), "proposal.md")); err != nil {
				t.Fatalf("recovered archive is incomplete: %v", err)
			}
			if records := historyKinds(t, root, record.KindArchived); len(records) != 1 {
				t.Fatalf("recovery left %d archive records", len(records))
			}
			// Recovery is repeatable: the same identity yields the same action.
			if err := RecoverTransactions(root, archiveClock); err != nil {
				t.Fatal(err)
			}
		})
	}
}
