package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

func TestTemplateEmbeddedShapes(t *testing.T) {
	templates := Templates()
	if len(templates) != 4 {
		t.Fatalf("embedded templates = %d", len(templates))
	}
	for _, name := range []TemplateName{ProposalTemplate, SpecTemplate, DesignTemplate, TasksTemplate} {
		raw := templates[name]
		if len(raw) == 0 || !bytes.Contains(raw, []byte("<")) {
			t.Fatalf("%s lacks embedded placeholder bytes", name)
		}
	}
}

func TestTemplateRawAndSubstitutedParity(t *testing.T) {
	root := templateRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(owner, "template-change", "sample"); err != nil {
		t.Fatal(err)
	}
	raw := ParseChange(owner, "template-change")
	for _, item := range raw.Diagnostics {
		if item.Code != "section_placeholder" && item.Code != "task_acceptance_placeholder" &&
			item.Code != "task_verify_placeholder" && item.Code != "delta_placeholder" {
			t.Fatalf("raw template has non-placeholder diagnostic: %#v", item)
		}
	}
	replacements := map[string]string{
		"<problem, e.g. Account updates can race.>":   "Updates can race.",
		"<outcome, e.g. Account updates serialize.>":  "Updates serialize.",
		"<scope, e.g. Local account updates.>":        "Local updates.",
		"<non-goal, e.g. Distributed locking.>":       "Distributed locks.",
		"<capability, e.g. accounts>":                 "sample",
		"<purpose>":                                   "purpose",
		"<requirement>":                               "requirement",
		"<scenario>":                                  "scenario",
		"<trigger>":                                   "trigger",
		"<result>":                                    "result",
		"<task id>":                                   "task",
		"<file>":                                      "file",
		"<verification>":                              "verification",
		"<acceptance>":                                "acceptance",
		"<boundary, e.g. Local sample store only.>":   "Local sample store only.",
		"<interface, e.g. One update transaction.>":   "One update transaction.",
		"<invariant, e.g. One writer owns the lock.>": "One writer owns the lock.",
		"<failure, e.g. Lock failure leaves bytes unchanged.>":                    "Failure leaves bytes unchanged.",
		"<integration, e.g. Existing update owner.>":                              "Sample update owner.",
		"<alternative, e.g. Distributed locking is unnecessary for one process.>": "Distributed lock is unnecessary for one process.",
		"<owner, e.g. internal/sample>":                                           "internal/sample",
	}
	for _, path := range []string{
		filepath.Join(root, ".specd", "changes", "template-change", "proposal.md"),
		filepath.Join(root, ".specd", "changes", "template-change", "design.md"),
		filepath.Join(root, ".specd", "changes", "template-change", "tasks.md"),
		filepath.Join(root, ".specd", "changes", "template-change", "specs", "sample", "spec.md"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for from, to := range replacements {
			text = strings.ReplaceAll(text, from, to)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parsed := ParseChange(owner, "template-change")
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("substituted templates drifted: %#v", parsed.Diagnostics)
	}
}

func TestTemplateRepeatedScaffoldPreservesBytes(t *testing.T) {
	root := templateRoot(t)
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(owner, "template-change", "sample"); err != nil {
		t.Fatal(err)
	}
	proposal := filepath.Join(root, ".specd", "changes", "template-change", "proposal.md")
	user := []byte("partial user bytes\n")
	if err := os.WriteFile(proposal, user, 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := Scaffold(owner, "template-change", "sample")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 || !bytes.Equal(after, user) {
		t.Fatalf("repeat scaffold changed authored bytes: %q, %#v", after, created)
	}
}

func TestTemplateScaffoldRequiresStageOneChange(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, ".specd"),
		filepath.Join(root, ".specd", "changes"),
		filepath.Join(root, ".specd", "specs"),
		filepath.Join(root, ".specd", "archive"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(owner, "absent-change", "sample"); err == nil {
		t.Fatal("scaffold created change without stage-1 state")
	}
	if _, err := os.Lstat(filepath.Join(root, ".specd", "changes", "absent-change")); !os.IsNotExist(err) {
		t.Fatalf("absent change was created: %v", err)
	}
}

func TestTemplateInterruptedScaffoldPreservesExisting(t *testing.T) {
	root := templateRoot(t)
	change := filepath.Join(root, ".specd", "changes", "template-change")
	if err := os.MkdirAll(change, 0o755); err != nil {
		t.Fatal(err)
	}
	proposal := filepath.Join(change, "proposal.md")
	user := []byte("do not overwrite\n")
	if err := os.WriteFile(proposal, user, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(change, "specs"), []byte("blocks directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Scaffold(owner, "template-change", "sample")
	if err == nil || !strings.Contains(err.Error(), "next: repair the path and retry scaffolding") {
		t.Fatalf("missing actionable failure: %v", err)
	}
	after, readErr := os.ReadFile(proposal)
	if readErr != nil || !bytes.Equal(after, user) {
		t.Fatalf("existing bytes changed: %q, %v", after, readErr)
	}
}

func TestTemplateWriteArtifactsUnlockedBytesAndDiagnostics(t *testing.T) {
	root := templateRoot(t)
	change := filepath.Join(root, ".specd", "changes", "template-change")
	created, err := WriteArtifacts(change, "sample")
	if err != nil || len(created) != 4 {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	for path, name := range map[string]TemplateName{
		filepath.Join(change, "proposal.md"):                ProposalTemplate,
		filepath.Join(change, "design.md"):                  DesignTemplate,
		filepath.Join(change, "tasks.md"):                   TasksTemplate,
		filepath.Join(change, "specs", "sample", "spec.md"): SpecTemplate,
	} {
		raw, readErr := os.ReadFile(path)
		want, templateErr := Template(name)
		if readErr != nil || templateErr != nil || !bytes.Equal(raw, want) {
			t.Fatalf("%s is not embedded template bytes: %v %v", path, readErr, templateErr)
		}
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range ParseChange(owner, "template-change").Diagnostics {
		if item.Code != "section_placeholder" && item.Code != "task_acceptance_placeholder" &&
			item.Code != "task_verify_placeholder" && item.Code != "delta_placeholder" {
			t.Fatalf("scaffolded change has non-placeholder diagnostic: %#v", item)
		}
	}
	// An unnamed capability scaffolds no delta spec.
	bare, err := WriteArtifacts(t.TempDir(), "")
	if err != nil || len(bare) != 3 {
		t.Fatalf("bare=%#v err=%v", bare, err)
	}
}

func TestTemplateDriftFailsCanonicalParser(t *testing.T) {
	raw, err := Template(TasksTemplate)
	if err != nil {
		t.Fatal(err)
	}
	drifted := bytes.Replace(raw, []byte("| acceptance |"), []byte("| unknown |"), 1)
	parsed := ParseTasks(Source{Path: "tasks.md", Bytes: drifted, Present: true})
	if !hasDiagnostic(parsed.Diagnostics, "task_table_missing") {
		t.Fatalf("unknown column escaped canonical parser: %#v", parsed.Diagnostics)
	}
}

func templateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, ".specd", "changes", "template-change"),
		filepath.Join(root, ".specd", "specs"),
		filepath.Join(root, ".specd", "archive"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := state.Encode(state.Initial("template-change", "test-creation"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".specd", "changes", "template-change", "state.json"), initial, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
