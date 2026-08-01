package plan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

func TestParseChangeMinimal(t *testing.T) {
	root := materializeChange(t, "minimal-change")
	accepted := filepath.Join(root, ".specd", "specs", "accounts")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	acceptedBytes := []byte("# Accounts\n\n## Requirements\n### Requirement: Stable locking\nThe system MUST lock.\n")
	if err := os.WriteFile(filepath.Join(accepted, "spec.md"), acceptedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "add-locking")
	if len(change.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", change.Diagnostics)
	}
	if len(change.Deltas) != 1 || len(change.Tasks.Tasks) != 1 ||
		len(change.Trace.Requirements) != 1 ||
		!slices.Equal(change.Trace.Requirements[0].Tasks, []string{"S2-001"}) {
		t.Fatalf("incomplete canonical model: %#v", change)
	}
}

func TestParseChangePartial(t *testing.T) {
	root := materializeChange(t, "partial-change")
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "unfinished")
	if !change.Proposal.Source.Present || change.Design.Source.Present || change.Tasks.Source.Present {
		t.Fatalf("artifact presence lost: %#v", change)
	}
	for _, code := range []string{"artifact_missing", "delta_missing", "section_placeholder"} {
		if !hasDiagnostic(change.Diagnostics, code) {
			t.Fatalf("missing %s: %#v", code, change.Diagnostics)
		}
	}
}

func TestParseChangeEmpty(t *testing.T) {
	root := t.TempDir()
	changeDir := filepath.Join(root, ".specd", "changes", "empty")
	for _, directory := range []string{
		changeDir, filepath.Join(root, ".specd", "specs"), filepath.Join(root, ".specd", "archive"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "empty")
	if change.Proposal.Source.Present || change.Design.Source.Present || change.Tasks.Source.Present ||
		!hasDiagnostic(change.Diagnostics, "artifact_missing") ||
		!hasDiagnostic(change.Diagnostics, "delta_missing") {
		t.Fatalf("empty change not inspectable: %#v", change)
	}
}

func TestParseChangeRejectsMissingChangeWithoutArtifactCascade(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".specd", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "absent")
	if !hasDiagnostic(change.Diagnostics, "change_path") ||
		hasDiagnostic(change.Diagnostics, "artifact_missing") {
		t.Fatalf("unsafe cascade: %#v", change.Diagnostics)
	}
}

func TestParseChangeRejectsSymlinkArtifactWithoutReadingOrCascade(t *testing.T) {
	root := materializeChange(t, "minimal-change")
	proposal := filepath.Join(root, ".specd", "changes", "add-locking", "proposal.md")
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside, []byte("## Problem\noutside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(proposal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, proposal); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "add-locking")
	if !hasDiagnostic(change.Diagnostics, "artifact_unsafe") ||
		hasDiagnosticAtPath(change.Diagnostics, "section_missing", proposal) {
		t.Fatalf("unsafe artifact cascade: %#v", change.Diagnostics)
	}
	if change.Proposal.Source.Present || len(change.Proposal.Sections) != 0 {
		t.Fatalf("unsafe artifact parsed: %#v", change.Proposal)
	}
}

func TestTraceUnknownAndUncovered(t *testing.T) {
	delta := CapabilityDelta{Capability: "accounts", Operations: []Operation{{
		Kind: Added, Requirement: &Requirement{
			Name: "Stable locking", Identity: NormalizeRequirementIdentity("Stable locking"),
			Location: Location{Path: "spec.md", Line: 3, Column: 1},
		},
	}}}
	tasks := Tasks{Tasks: []Task{{
		ID: "T1", Valid: true, Location: Location{Path: "tasks.md", Line: 3, Column: 1},
		References: []RequirementReference{{
			Capability: "accounts", Requirement: "Missing",
			Location: Location{Path: "tasks.md", Line: 3, Column: 1},
		}},
	}}}
	trace, diagnostics := projectTrace([]CapabilityDelta{delta}, Design{}, tasks)
	if len(trace.Requirements) != 1 || !hasDiagnostic(diagnostics, "trace_unknown") ||
		!hasDiagnostic(diagnostics, "trace_uncovered") {
		t.Fatalf("trace did not fail closed: %#v %#v", trace, diagnostics)
	}
}

func TestTraceResolvesDesignReferencesDespiteSectionDiagnostics(t *testing.T) {
	delta := CapabilityDelta{Capability: "accounts", Operations: []Operation{{
		Kind: Added, Requirement: &Requirement{
			Name: "Stable locking", Identity: NormalizeRequirementIdentity("Stable locking"),
		},
	}}}
	design := Design{
		References: []RequirementReference{{
			Capability: "accounts", Requirement: "Missing",
			Location: Location{Path: "design.md", Line: 9, Column: 1},
		}},
		// Unrelated section defect, exactly what the shipped template produces.
		Diagnostics: []Diagnostic{diagnostic(
			"section_placeholder", Location{Path: "design.md", Line: 4, Column: 1},
			"Boundaries section contains scaffold placeholder text", "replace placeholder with concrete content",
		)},
	}
	_, diagnostics := projectTrace([]CapabilityDelta{delta}, design, Tasks{})
	if !hasDiagnostic(diagnostics, "trace_unknown") {
		t.Fatalf("unrelated design diagnostic suppressed reference resolution: %#v", diagnostics)
	}
}

func TestTraceInvalidTaskCannotCoverRequirement(t *testing.T) {
	delta := CapabilityDelta{Capability: "accounts", Operations: []Operation{{
		Kind: Added, Requirement: &Requirement{Name: "Stable locking"},
	}}}
	tasks := Tasks{Tasks: []Task{{
		ID: "T1", Valid: false, References: []RequirementReference{{
			Capability: "accounts", Requirement: "Stable locking",
		}},
	}}}
	trace, diagnostics := projectTrace([]CapabilityDelta{delta}, Design{}, tasks)
	if len(trace.Tasks) != 0 || !hasDiagnostic(diagnostics, "trace_uncovered") {
		t.Fatalf("invalid task covered requirement: %#v %#v", trace, diagnostics)
	}
}

func TestTraceAmbiguous(t *testing.T) {
	deltas := []CapabilityDelta{
		{Capability: "accounts", Operations: []Operation{{
			Kind: Added, Requirement: &Requirement{Name: "Stable locking"},
		}}},
		{Capability: "accounts", Operations: []Operation{{
			Kind: Modified, Requirement: &Requirement{Name: " stable   LOCKING "},
		}}},
	}
	tasks := Tasks{Tasks: []Task{{
		ID: "T1", Valid: true, References: []RequirementReference{{
			Capability: "accounts", Requirement: "Stable locking",
			Location: Location{Path: "tasks.md", Line: 3, Column: 1},
		}},
	}}}
	_, diagnostics := projectTrace(deltas, Design{}, tasks)
	if !hasDiagnostic(diagnostics, "trace_ambiguous") {
		t.Fatalf("ambiguous identity trusted: %#v", diagnostics)
	}
}

func TestTracePreservesAuthoredOrderAndRenameDestination(t *testing.T) {
	deltas := []CapabilityDelta{
		{Capability: "zeta", Operations: []Operation{{
			Kind: Added, Requirement: &Requirement{Name: "First"},
		}}},
		{Capability: "alpha", Operations: []Operation{{
			Kind: Added, Requirement: &Requirement{Name: "Second"},
		}, {
			Kind: Renamed, From: "Old name", To: "New name",
		}}},
	}
	tasks := Tasks{Tasks: []Task{
		{ID: "T2", Valid: true, References: []RequirementReference{{Capability: "alpha", Requirement: "Second"}}},
		{ID: "T1", Valid: true, References: []RequirementReference{{Capability: "zeta", Requirement: "First"}}},
		{ID: "T3", Valid: true, References: []RequirementReference{{Capability: "alpha", Requirement: "New name"}}},
	}}
	trace, diagnostics := projectTrace(deltas, Design{}, tasks)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	got := []string{}
	for _, requirement := range trace.Requirements {
		got = append(got, requirement.Capability+":"+requirement.Requirement)
	}
	want := []string{"zeta:First", "alpha:Second", "alpha:New name"}
	if !slices.Equal(got, want) {
		t.Fatalf("trace order/rename = %#v, want %#v", got, want)
	}
}

func TestSharedProjection(t *testing.T) {
	change := &Change{Name: "one-model"}
	if change.Inspection().Canonical != change || change.Validation().Canonical != change {
		t.Fatal("projections did not share canonical model")
	}
}

func materializeChange(t *testing.T, fixture string) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("testdata", fixture)
	target := filepath.Join(root, ".specd", "changes", map[string]string{
		"minimal-change": "add-locking", "partial-change": "unfinished",
	}[fixture])
	if err := copyFixture(source, target); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{
		filepath.Join(root, ".specd", "specs"),
		filepath.Join(root, ".specd", "archive"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func copyFixture(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, raw, 0o644)
	})
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, item := range diagnostics {
		if item.Code == code {
			return true
		}
	}
	return false
}

func hasDiagnosticAtPath(diagnostics []Diagnostic, code, path string) bool {
	for _, item := range diagnostics {
		if item.Code == code && item.Location.Path == path {
			return true
		}
	}
	return false
}
