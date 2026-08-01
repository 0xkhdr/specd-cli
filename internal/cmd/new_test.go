package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestNew(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	result, err := New(root, "cache-ttl", "tester", "sample")
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || result.Stage != "planning" {
		t.Fatalf("result = %+v", result)
	}
	records, _, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	if _, err := New(root, "cache-ttl", "tester", "sample"); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestNewScaffoldsPlanningArtifacts(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "cache-ttl", "tester", "cache-ttl"); err != nil {
		t.Fatal(err)
	}
	change := filepath.Join(root, ".specd", "changes", "cache-ttl")
	for path, name := range map[string]plan.TemplateName{
		filepath.Join(change, "proposal.md"):                   plan.ProposalTemplate,
		filepath.Join(change, "design.md"):                     plan.DesignTemplate,
		filepath.Join(change, "tasks.md"):                      plan.TasksTemplate,
		filepath.Join(change, "specs", "cache-ttl", "spec.md"): plan.SpecTemplate,
	} {
		raw, readErr := os.ReadFile(path)
		want, templateErr := plan.Template(name)
		if readErr != nil || templateErr != nil || !bytes.Equal(raw, want) {
			t.Fatalf("%s is not embedded template bytes: %v %v", path, readErr, templateErr)
		}
	}
}

func TestNewRecoversOrphanStagingDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	// A crash between staging and journalling leaves an unpublishable directory.
	stage := filepath.Join(root, ".specd", ".new-cache-ttl")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "cache-ttl", "tester", "cache-ttl"); err != nil {
		t.Fatalf("retry after orphan staging directory: %v", err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("staging directory remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".specd", "changes", "cache-ttl", "proposal.md")); err != nil {
		t.Fatalf("retry did not scaffold: %v", err)
	}
}

func TestNewArtifactFailureLeavesNoChange(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	// A valid but unwritable capability segment fails at the artifact step,
	// after the staged state bytes are already written.
	long := strings.Repeat("c", 400)
	_, err := New(root, "cache-ttl", "tester", long)
	if !failure.IsCode(err, "scaffold_failed") {
		t.Fatalf("artifact failure = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".specd", "changes", "cache-ttl")); !os.IsNotExist(err) {
		t.Fatalf("half-created change remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".specd", ".new-cache-ttl")); !os.IsNotExist(err) {
		t.Fatalf("staging directory remains: %v", err)
	}
	records, _, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
}

func TestNewRecoversPublishedTransaction(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "first", "tester", "sample"); err != nil {
		t.Fatal(err)
	}
	// A leftover committed journal is safe and idempotently removed.
	records, _, _ := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	tx := creationTransaction{Change: "first", Record: records[0]}
	if err := writeTransaction(mustOwner(t, root), tx); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".specd", ".new-first.json")); !os.IsNotExist(err) {
		t.Fatalf("journal remains: %v", err)
	}
}

func TestNewRefusesManagedSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".specd")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "escape", "tester", "sample"); err == nil {
		t.Fatal("managed symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, ".root.lock")); !os.IsNotExist(err) {
		t.Fatalf("outside lock created: %v", err)
	}
}
