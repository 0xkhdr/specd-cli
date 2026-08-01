package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestScopeExactDeclaredAndEmptyDiff(t *testing.T) {
	root := attemptRoot(t)
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	result, err := CheckScope(root, "safe-change", "T1", attempt.ID)
	if err != nil || !result.Valid || len(result.ChangedPaths) != 0 ||
		result.Assurance != AttemptAssurance {
		t.Fatalf("empty scope = %#v, %v", result, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "sample.go"), []byte("package internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = CheckScope(root, "safe-change", "T1", attempt.ID)
	if err != nil || !reflect.DeepEqual(result.ChangedPaths, []string{"internal/sample.go"}) {
		t.Fatalf("declared scope = %#v, %v", result, err)
	}
}

func TestScopeRejectsOutsideDeleteRenameSymlinkAndManaged(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) (string, Attempt)
		edit  func(*testing.T, string)
		code  string
		paths []string
	}{
		{"sibling", startedAttemptRoot, func(t *testing.T, root string) {
			mustWriteScopeFile(t, filepath.Join(root, "internal", "sibling.go"), "package internal\n")
		}, "scope_outside", []string{"internal/sibling.go"}},
		{"untracked", startedAttemptRoot, func(t *testing.T, root string) {
			mustWriteScopeFile(t, filepath.Join(root, "new.txt"), "new\n")
		}, "scope_outside", []string{"new.txt"}},
		{"managed", startedAttemptRoot, func(t *testing.T, root string) {
			mustWriteScopeFile(t, filepath.Join(root, ".specd", "rogue"), "rogue\n")
		}, "scope_outside", []string{".specd/rogue"}},
		{"deleted", startedTrackedAttemptRoot, func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "internal", "sample.go")); err != nil {
				t.Fatal(err)
			}
		}, "scope_outside", []string{"internal/sample.go"}},
		{"renamed", startedTrackedAttemptRoot, func(t *testing.T, root string) {
			if err := os.Rename(
				filepath.Join(root, "internal", "sample.go"),
				filepath.Join(root, "internal", "renamed.go"),
			); err != nil {
				t.Fatal(err)
			}
		}, "scope_outside", []string{"internal/renamed.go", "internal/sample.go"}},
		{"symlink", startedAttemptRoot, func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.go")
			mustWriteScopeFile(t, outside, "outside\n")
			if err := os.Symlink(outside, filepath.Join(root, "internal", "sample.go")); err != nil {
				t.Fatal(err)
			}
		}, "attempt_scope", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, attempt := test.setup(t)
			test.edit(t, root)
			statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
			evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeEvidence, _ := os.ReadFile(evidencePath)
			_, err := CheckScope(root, "safe-change", "T1", attempt.ID)
			if !failure.IsCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
			for _, path := range test.paths {
				if !strings.Contains(err.Error(), path) {
					t.Fatalf("error %q missing %q", err, path)
				}
			}
			afterState, _ := os.ReadFile(statePath)
			afterEvidence, _ := os.ReadFile(evidencePath)
			if string(beforeState) != string(afterState) || string(beforeEvidence) != string(afterEvidence) {
				t.Fatal("scope refusal mutated state or evidence")
			}
		})
	}
}

func TestScopeRejectsIdentityAndInputDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(*testing.T, string, Attempt)
		id   func(Attempt) string
	}{
		{"attempt id", func(*testing.T, string, Attempt) {}, func(Attempt) string { return "wrong" }},
		{"contract", func(t *testing.T, root string, _ Attempt) {
			rewriteAttemptTasks(t, root, "internal/other.go")
		}, func(attempt Attempt) string { return attempt.ID }},
		{"approval", func(t *testing.T, root string, _ Attempt) {
			appendAttemptFile(t,
				filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md"),
				[]byte("\ndrift\n"),
			)
		}, func(attempt Attempt) string { return attempt.ID }},
		{"revision", func(t *testing.T, root string, _ Attempt) {
			statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
			current := readTaskTransitionState(t, root)
			current.Revision++
			raw, err := state.Encode(current)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}, func(attempt Attempt) string { return attempt.ID }},
		{"head", func(t *testing.T, root string, _ Attempt) {
			mustWriteScopeFile(t, filepath.Join(root, "post-attempt.txt"), "commit\n")
			runAttemptGit(t, root, "add", "post-attempt.txt")
			runAttemptGit(t, root, "commit", "-m", "move head", "--no-gpg-sign")
		}, func(attempt Attempt) string { return attempt.ID }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, attempt := startedAttemptRoot(t)
			test.edit(t, root, attempt)
			if _, err := CheckScope(root, "safe-change", "T1", test.id(attempt)); err == nil {
				t.Fatal("drift passed")
			}
		})
	}
}

func TestScopeMalformedGitNameStatusFailsClosed(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte("M"), []byte("Q\x00file\x00"), []byte("R\x00old\x00new\x00"),
		[]byte("R100\x00old\x00"), []byte("M100\x00file\x00"),
		[]byte("M\x00../escape\x00"), {0xff, 0},
	} {
		if _, err := parseGitNameStatus(raw); err == nil {
			t.Fatalf("malformed Git output accepted: %q", raw)
		}
	}
}

func TestScopeIncludesIgnoredUntrackedPaths(t *testing.T) {
	t.Run("declared", func(t *testing.T) {
		root := attemptRoot(t)
		appendAttemptFile(t, filepath.Join(root, ".gitignore"), []byte("internal/sample.go\n"))
		runAttemptGit(t, root, "add", ".gitignore")
		runAttemptGit(t, root, "commit", "-m", "ignore declared output", "--no-gpg-sign")
		attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
		if err != nil {
			t.Fatal(err)
		}
		mustWriteScopeFile(t, filepath.Join(root, "internal", "sample.go"), "package internal\n")
		result, err := CheckScope(root, "safe-change", "T1", attempt.ID)
		if err != nil || !reflect.DeepEqual(result.ChangedPaths, []string{"internal/sample.go"}) {
			t.Fatalf("ignored declared path = %#v, %v", result, err)
		}
	})
	t.Run("undeclared", func(t *testing.T) {
		root := attemptRoot(t)
		appendAttemptFile(t, filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"))
		runAttemptGit(t, root, "add", ".gitignore")
		runAttemptGit(t, root, "commit", "-m", "ignore sibling", "--no-gpg-sign")
		attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
		if err != nil {
			t.Fatal(err)
		}
		mustWriteScopeFile(t, filepath.Join(root, "ignored.txt"), "ignored\n")
		if _, err := CheckScope(root, "safe-change", "T1", attempt.ID); !failure.IsCode(err, "scope_outside") ||
			!strings.Contains(err.Error(), "ignored.txt") {
			t.Fatalf("ignored undeclared error = %v", err)
		}
	})
}

func TestScopeIgnoresHarnessLocks(t *testing.T) {
	// Locks reach the change list two ways: untracked when the managed tree is
	// not committed, ignored when .specd/.gitignore excludes them. Neither is a
	// content write, so neither may refuse a scoped completion.
	for _, name := range []string{"untracked", "ignored"} {
		t.Run(name, func(t *testing.T) {
			root := attemptRoot(t)
			if name == "ignored" {
				mustWriteScopeFile(t, filepath.Join(root, ".specd", ".gitignore"), ".root.lock\n.records.lock\nchanges/*/.lock\n")
				runAttemptGit(t, root, "add", "-f", ".specd/.gitignore")
				runAttemptGit(t, root, "commit", "-m", "ignore harness locks", "--no-gpg-sign")
			}
			attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
			if err != nil {
				t.Fatal(err)
			}
			mustWriteScopeFile(t, filepath.Join(root, "internal", "sample.go"), "package internal\n")
			for _, lock := range []string{
				filepath.Join(root, ".specd", ".root.lock"),
				filepath.Join(root, ".specd", ".records.lock"),
				filepath.Join(root, ".specd", "changes", "safe-change", ".lock"),
			} {
				if err := os.WriteFile(lock, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := CheckScope(root, "safe-change", "T1", attempt.ID)
			if err != nil || !reflect.DeepEqual(result.ChangedPaths, []string{"internal/sample.go"}) {
				t.Fatalf("harness locks changed scope: %#v, %v", result, err)
			}
		})
	}
}

func TestScopeAllowsOnlyValidatedTrackedAttemptMetadata(t *testing.T) {
	root := attemptRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runAttemptGit(t, root, "add", "-f", ".gitignore", ".specd")
	runAttemptGit(t, root, "commit", "-m", "track harness metadata", "--no-gpg-sign")
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := CheckScope(root, "safe-change", "T1", attempt.ID); err != nil || !result.Valid {
		t.Fatalf("validated attempt metadata = %#v, %v", result, err)
	}

	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	current := readTaskTransitionState(t, root)
	current.Extensions["tampered"] = []byte(`true`)
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckScope(root, "safe-change", "T1", attempt.ID); !failure.IsCode(err, "scope_drift") {
		t.Fatalf("direct managed state mutation = %v", err)
	}
}

func startedAttemptRoot(t *testing.T) (string, Attempt) {
	t.Helper()
	root := attemptRoot(t)
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	return root, attempt
}

func startedTrackedAttemptRoot(t *testing.T) (string, Attempt) {
	t.Helper()
	root := attemptRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteScopeFile(t, filepath.Join(root, "internal", "sample.go"), "package internal\n")
	runAttemptGit(t, root, "add", "internal/sample.go")
	runAttemptGit(t, root, "commit", "-m", "tracked implementation", "--no-gpg-sign")
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	return root, attempt
}

func mustWriteScopeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
