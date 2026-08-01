package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

func TestDeltaAllOperations(t *testing.T) {
	raw := []byte("## ADDED Requirements\n### Requirement: Added behavior\nThe system SHALL add.\n#### Scenario: Added\n- **WHEN** invoked\n- **THEN** added\n## MODIFIED Requirements\n### Requirement: Existing behavior\nThe system MUST change fully.\n#### Scenario: Changed\n- WHEN invoked\n- THEN changed\n## REMOVED Requirements\n### Requirement: Old behavior\n**Reason**: obsolete\n**Migration**: use Existing behavior\n## RENAMED Requirements\n- FROM: `### Requirement: Earlier name`\n- TO: `### Requirement: Later name`\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: raw, Present: true}, "cache-policy", acceptedWith("Existing behavior", "Old behavior", "Earlier name"))
	if len(got.Diagnostics) != 0 || len(got.Operations) != 4 {
		t.Fatalf("unexpected delta: %#v", got)
	}
	for i, want := range []DeltaOperation{Added, Modified, Removed, Renamed} {
		if got.Operations[i].Kind != want {
			t.Fatalf("operation %d = %s", i, got.Operations[i].Kind)
		}
	}
	if got.Operations[0].Requirement.Identity != "added behavior" || got.Operations[0].Requirement.Scenarios[0].Location.Line != 4 {
		t.Fatalf("identity/location lost: %#v", got.Operations[0])
	}
}

func TestDeltaNewCapabilityPurpose(t *testing.T) {
	valid := []byte("## Purpose\nCache policy.\n## ADDED Requirements\n### Requirement: New policy\nThe system SHALL cache.\n#### Scenario: Cache\n- WHEN requested\n- THEN cached\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: valid, Present: true}, "cache-policy", acceptedNone)
	if len(got.Diagnostics) != 0 || !bytes.Equal(got.Source.Bytes, valid) {
		t.Fatalf("valid new capability = %#v", got)
	}
	existing := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: valid, Present: true}, "cache-policy", acceptedWith("Other policy"))
	assertHasCode(t, existing.Diagnostics, "purpose_existing")
}

func TestDeltaMalformedAndConflicting(t *testing.T) {
	raw := []byte("## ADDED Requirements\n### Requirement: Cache   Policy\nThe system should cache.\n#### Scenario: Weak\n- GIVEN input\n## REMOVED Requirements\n### Requirement: cache policy\n**Reason**: duplicate\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: raw, Present: true}, "cache-policy", acceptedWith("Cache policy"))
	for _, code := range []string{"requirement_normative", "scenario_steps", "removed_incomplete"} {
		assertHasCode(t, got.Diagnostics, code)
	}
	if len(got.Operations) != 0 {
		t.Fatalf("malformed operations synthesized: %#v", got.Operations)
	}

	conflict := []byte("## ADDED Requirements\n### Requirement: Cache   Policy\nThe system SHALL cache.\n#### Scenario: Cache\n- WHEN requested\n- THEN cached\n## REMOVED Requirements\n### Requirement: cache policy\n**Reason**: duplicate\n**Migration**: none\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: conflict, Present: true}, "cache-policy", acceptedWith("Cache policy"))
	assertHasCode(t, got.Diagnostics, "requirement_conflict")

	fencedOnly := []byte("## ADDED Requirements\n### Requirement: Fence is prose\n```markdown\nThe system SHALL not count this.\n#### Scenario: Fake\n- WHEN fake\n- THEN fake\n```\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: fencedOnly, Present: true}, "cache-policy", acceptedWith("Fence is prose"))
	assertHasCode(t, got.Diagnostics, "requirement_normative")
	assertHasCode(t, got.Diagnostics, "requirement_scenario")
}

func TestDeltaFencesLevelsAndLineEndings(t *testing.T) {
	lf := []byte("## ADDED Requirements\n### Requirement: Real\nThe system SHALL work.\n```markdown\n### Requirement: Fake\n#### Scenario: Fake\n```\n#### Scenario: Real\n- WHEN run\n- THEN works\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	a := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: lf, Present: true}, "parser", acceptedWith("Other"))
	b := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: crlf, Present: true}, "parser", acceptedWith("Other"))
	if len(a.Diagnostics) != 0 || len(b.Diagnostics) != 0 || len(a.Operations) != 1 || len(b.Operations) != 1 {
		t.Fatalf("fence/line endings drift: %#v %#v", a, b)
	}
	if bytes.Equal(a.Source.Bytes, b.Source.Bytes) {
		t.Fatal("source line endings not preserved")
	}
	if !reflect.DeepEqual(operationSemantics(a.Operations), operationSemantics(b.Operations)) {
		t.Fatal("semantic line-ending drift")
	}

	wrong := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: []byte("## ADDED Requirements\n### Requirement: Real\nThe system SHALL work.\n### Scenario: Wrong\n- WHEN run\n- THEN works\n"), Present: true}, "parser", acceptedWith("Other"))
	assertHasCode(t, wrong.Diagnostics, "scenario_level")
	if countCode(wrong.Diagnostics, "scenario_level") != 1 {
		t.Fatalf("duplicate scenario diagnostics: %#v", wrong.Diagnostics)
	}
	if hasCode(wrong.Diagnostics, "requirement_name") {
		t.Fatalf("wrong scenario level became name error: %#v", wrong.Diagnostics)
	}
	mixed := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: []byte("## ADDED Requirements\n### Requirement: Broken\nThe system SHALL break.\n### Scenario: Wrong\n- WHEN run\n- THEN breaks\n### Requirement: Sound\nThe system SHALL work.\n#### Scenario: Right\n- WHEN run\n- THEN works\n"), Present: true}, "parser", acceptedWith("Other"))
	if len(mixed.Operations) != 1 || mixed.Operations[0].Requirement.Name != "Sound" {
		t.Fatalf("wrong scenario level invalidated sibling: %#v", mixed)
	}
}

func TestDeltaFencedRemovalAndRenameStayProse(t *testing.T) {
	removed := []byte("## REMOVED Requirements\n### Requirement: Old\n```md\n**Reason**: fake\n**Migration**: fake\n```\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: removed, Present: true}, "parser", acceptedWith("Old"))
	assertHasCode(t, got.Diagnostics, "removed_incomplete")
	if len(got.Operations) != 0 {
		t.Fatalf("fenced removal synthesized: %#v", got.Operations)
	}

	renamed := []byte("## RENAMED Requirements\n```md\n- FROM: `### Requirement: Old`\n- TO: `### Requirement: New`\n```\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: renamed, Present: true}, "parser", acceptedWith("Old"))
	assertHasCode(t, got.Diagnostics, "rename_incomplete")
	if len(got.Operations) != 0 {
		t.Fatalf("fenced rename synthesized: %#v", got.Operations)
	}
}

func TestDeltaDuplicateNamesPriorLocationAndModifiedFullBlock(t *testing.T) {
	duplicate := []byte("## ADDED Requirements\n### Requirement: Cache   Policy\nThe system SHALL cache.\n#### Scenario: First\n- WHEN read\n- THEN cached\n### Requirement: cache policy\nThe system MUST cache.\n#### Scenario: Second\n- WHEN read\n- THEN cached\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: duplicate, Present: true}, "parser", acceptedWith("Other"))
	assertHasCode(t, got.Diagnostics, "requirement_duplicate")
	for _, item := range got.Diagnostics {
		if item.Code == "requirement_duplicate" && !strings.Contains(item.Message, "spec.md:2") {
			t.Fatalf("prior location missing: %#v", item)
		}
	}

	incomplete := []byte("## MODIFIED Requirements\n### Requirement: Cache policy\nThe system should cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: incomplete, Present: true}, "parser", acceptedWith("Cache policy"))
	assertHasCode(t, got.Diagnostics, "requirement_normative")
	if len(got.Operations) != 0 {
		t.Fatalf("incomplete replacement synthesized: %#v", got.Operations)
	}
}

func TestDeltaDiscoveryUsesStageOnePaths(t *testing.T) {
	root := t.TempDir()
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	specDir, _ := owner.ChangeSpec("change-one", "cache-policy")
	if err := os.MkdirAll(filepath.Dir(specDir), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("## Purpose\nCache.\n## ADDED Requirements\n### Requirement: Cache\nThe system SHALL cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	if err := os.WriteFile(specDir, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	deltas, diagnostics := DiscoverCapabilityDeltas(owner, "change-one")
	if len(diagnostics) != 0 || len(deltas) != 1 || len(deltas[0].Diagnostics) != 0 {
		t.Fatalf("discovery = %#v %#v", deltas, diagnostics)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(specDir)), "spec.md"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = DiscoverCapabilityDeltas(owner, "change-one")
	assertHasCode(t, diagnostics, "capability_path")

	if err := os.Remove(filepath.Join(filepath.Dir(filepath.Dir(specDir)), "spec.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(specDir), "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = DiscoverCapabilityDeltas(owner, "change-one")
	assertHasCode(t, diagnostics, "capability_path")
}

func TestDeltaDiscoveryRejectsInvalidAcceptedSpecKind(t *testing.T) {
	root := t.TempDir()
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := owner.ChangeSpec("change-one", "cache-policy")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("## Purpose\nCache.\n## ADDED Requirements\n### Requirement: Cache\nThe system SHALL cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	accepted, _ := owner.AcceptedSpec("cache-policy")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := DiscoverCapabilityDeltas(owner, "change-one")
	assertHasCode(t, diagnostics, "accepted_path")
}

func TestDeltaAcceptedIdentityGate(t *testing.T) {
	accepted := acceptedWith("Cache policy")
	unknown := []byte("## MODIFIED Requirements\n### Requirement: Cache pruning\nThe system MUST prune.\n#### Scenario: Prune\n- WHEN full\n- THEN pruned\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: unknown, Present: true}, "cache-policy", accepted)
	assertHasCode(t, got.Diagnostics, "requirement_unknown")

	known := []byte("## MODIFIED Requirements\n### Requirement: cache   POLICY\nThe system MUST cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: known, Present: true}, "cache-policy", accepted)
	if len(got.Diagnostics) != 0 || len(got.Operations) != 1 {
		t.Fatalf("existing requirement refused: %#v", got)
	}

	collision := []byte("## ADDED Requirements\n### Requirement: Cache policy\nThe system MUST cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: collision, Present: true}, "cache-policy", accepted)
	assertHasCode(t, got.Diagnostics, "requirement_exists")

	// A fenced accepted heading is prose, so it grants no identity.
	fenced := Source{Path: "accepted.md", Present: true, Bytes: []byte("## Requirements\n```md\n### Requirement: Cache pruning\n```\n")}
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: unknown, Present: true}, "cache-policy", fenced)
	assertHasCode(t, got.Diagnostics, "requirement_unknown")

	// New capability: no accepted spec means no identity gate at all.
	newCapability := []byte("## Purpose\nCache.\n## ADDED Requirements\n### Requirement: Cache policy\nThe system MUST cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	got = ParseCapabilityDelta(Source{Path: "spec.md", Bytes: newCapability, Present: true}, "cache-policy", acceptedNone)
	if len(got.Diagnostics) != 0 || len(got.Operations) != 1 {
		t.Fatalf("new capability changed: %#v", got)
	}
}

func TestDeltaNewCapabilityRefusesOperationWithoutSynthesizing(t *testing.T) {
	raw := []byte("## Purpose\nCache.\n## MODIFIED Requirements\n### Requirement: Cache policy\nThe system MUST cache.\n#### Scenario: Cache\n- WHEN read\n- THEN cached\n")
	got := ParseCapabilityDelta(Source{Path: "spec.md", Bytes: raw, Present: true}, "cache-policy", acceptedNone)
	assertHasCode(t, got.Diagnostics, "operation_new_capability")
	if len(got.Operations) != 0 {
		t.Fatalf("refused operation synthesized: %#v", got.Operations)
	}
}

func acceptedWith(names ...string) Source {
	raw := "# Capability\n\n## Requirements\n"
	for _, name := range names {
		raw += "### Requirement: " + name + "\nThe system MUST behave.\n#### Scenario: Accepted\n- WHEN read\n- THEN behaves\n"
	}
	return Source{Path: "accepted.md", Present: true, Bytes: []byte(raw)}
}

var acceptedNone = Source{Path: "accepted.md"}

func assertHasCode(t *testing.T, diagnostics []Diagnostic, code string) {
	t.Helper()
	for _, item := range diagnostics {
		if item.Code == code {
			return
		}
	}
	t.Fatalf("missing %s in %#v", code, diagnostics)
}

func countCode(diagnostics []Diagnostic, code string) int {
	count := 0
	for _, item := range diagnostics {
		if item.Code == code {
			count++
		}
	}
	return count
}

func operationSemantics(operations []Operation) []string {
	var result []string
	for _, operation := range operations {
		result = append(result, string(operation.Kind)+":"+operation.Requirement.Identity)
	}
	return result
}
