package reconcile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const change = "cache-work"

func newOwner(t *testing.T) *corepath.Owner {
	t.Helper()
	owner, err := corepath.New(t.TempDir())
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	return owner
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeAccepted(t *testing.T, owner *corepath.Owner, capability, content string) {
	t.Helper()
	path, err := owner.AcceptedSpec(capability)
	if err != nil {
		t.Fatalf("accepted path: %v", err)
	}
	write(t, path, content)
}

func writeDelta(t *testing.T, owner *corepath.Owner, capability, content string) {
	t.Helper()
	path, err := owner.ChangeSpec(change, capability)
	if err != nil {
		t.Fatalf("delta path: %v", err)
	}
	write(t, path, content)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "preservation", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(raw)
}

func codes(diagnostics []plan.Diagnostic) []string {
	found := make([]string, 0, len(diagnostics))
	for _, item := range diagnostics {
		found = append(found, item.Code)
	}
	return found
}

func assertCode(t *testing.T, result Plan, code string) {
	t.Helper()
	for _, item := range result.Diagnostics {
		if item.Code == code {
			if item.Repair == "" || item.Location.Path == "" {
				t.Fatalf("diagnostic %s has no path or repair: %#v", code, item)
			}
			return
		}
	}
	t.Fatalf("missing diagnostic %s in %v", code, codes(result.Diagnostics))
}

func assertWithheld(t *testing.T, result Plan) {
	t.Helper()
	if result.Applicable || result.NoOp {
		t.Fatalf("conflicting plan is applicable: %#v", result)
	}
	for _, capability := range result.Capabilities {
		if capability.Output != nil || capability.OutputHash != "" {
			t.Fatalf("output not withheld for %s", capability.Capability)
		}
	}
}

const acceptedOne = "# Cache policy\n\n## Purpose\n\nCaching.\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n"

func TestPlanEmptyDeltaSetIsDeterministicNoOp(t *testing.T) {
	owner := newOwner(t)
	result := Build(owner, change)
	if !result.Applicable || !result.NoOp || len(result.Capabilities) != 0 || len(result.Diagnostics) != 0 {
		t.Fatalf("empty change is not a no-op plan: %#v", result)
	}
}

func TestPlanRepeatedRunsAreIdentical(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", fixture(t, "accepted.md"))
	writeDelta(t, owner, "cache-policy", fixture(t, "delta.md"))
	first, second := Build(owner, change), Build(owner, change)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("planning is not deterministic:\n%#v\n%#v", first, second)
	}
	if !first.Applicable || first.NoOp || len(first.Capabilities) != 1 {
		t.Fatalf("unexpected plan: %#v", first)
	}
	capability := first.Capabilities[0]
	if capability.AcceptedHash == "" || capability.DeltaHash == "" ||
		capability.OutputHash == "" || capability.OutputHash == capability.AcceptedHash {
		t.Fatalf("hashes are missing or unchanged: %#v", capability)
	}
}

func TestPlanNewCapabilityAndIdempotence(t *testing.T) {
	owner := newOwner(t)
	writeDelta(t, owner, "cache-policy", "## Purpose\n\nCaching.\n\n## ADDED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	result := Build(owner, change)
	if !result.Applicable || result.NoOp || !result.Capabilities[0].Created {
		t.Fatalf("new capability plan: %#v", result)
	}
	created := result.Capabilities[0].Output
	if !bytes.Contains(created, []byte("# Cache policy")) ||
		!bytes.Contains(created, []byte("## Purpose")) ||
		!bytes.Contains(created, []byte("### Requirement: Existing behavior")) {
		t.Fatalf("rebuilt new capability:\n%s", created)
	}

	// Applying the same effect twice is a no-op, not a fabricated write.
	writeAccepted(t, owner, "cache-policy", string(created))
	writeDelta(t, owner, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	again := Build(owner, change)
	if !again.Applicable || !again.NoOp {
		t.Fatalf("synchronized plan is not a no-op: %#v", again)
	}
	if !bytes.Equal(again.Capabilities[0].Output, created) {
		t.Fatalf("no-op plan changed bytes:\n%s", again.Capabilities[0].Output)
	}
}

func TestPlanRefusesUnsafeAmbiguousAndDeltaHeadings(t *testing.T) {
	owner := newOwner(t)
	specs, err := owner.ChangeSpecs(change)
	if err != nil {
		t.Fatalf("specs: %v", err)
	}
	write(t, filepath.Join(specs, "spec.md"), "# stray\n")
	assertCode(t, Build(owner, change), "capability_path")

	deltaHeadings := newOwner(t)
	writeAccepted(t, deltaHeadings, "cache-policy", acceptedOne+"\n## ADDED Requirements\n\n### Requirement: Sneaky\n")
	writeDelta(t, deltaHeadings, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache twice.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	result := Build(deltaHeadings, change)
	assertCode(t, result, "accepted_delta_heading")
	assertWithheld(t, result)

	duplicate := newOwner(t)
	writeAccepted(t, duplicate, "cache-policy", acceptedOne+"\n### Requirement: existing   behavior\n\nThe system MUST cache.\n\n#### Scenario: Again\n- **WHEN** requested\n- **THEN** cached\n")
	writeDelta(t, duplicate, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache once.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	assertCode(t, Build(duplicate, change), "accepted_duplicate")

	unsafe := newOwner(t)
	path, err := unsafe.AcceptedSpec("cache-policy")
	if err != nil {
		t.Fatalf("accepted path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join(unsafe.Root(), "outside.md"), path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	write(t, filepath.Join(unsafe.Root(), "outside.md"), acceptedOne)
	writeDelta(t, unsafe, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache once.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	// Discovery refuses the symlink first; the read guard below covers a swap
	// that lands between discovery and the accepted read.
	assertCode(t, Build(unsafe, change), "accepted_path")
	if _, diagnostics := readAccepted(path); len(diagnostics) != 1 || diagnostics[0].Code != "accepted_unsafe" {
		t.Fatalf("accepted read guard = %v", codes(diagnostics))
	}
}

func TestOperationsApplyEveryKind(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", fixture(t, "accepted.md"))
	writeDelta(t, owner, "cache-policy", fixture(t, "delta.md"))
	result := Build(owner, change)
	if !result.Applicable {
		t.Fatalf("plan refused: %v", codes(result.Diagnostics))
	}
	want := []Operation{
		{Kind: plan.Modified, Requirement: "Replace me"},
		{Kind: plan.Removed, Requirement: "Drop me"},
		{Kind: plan.Renamed, Requirement: "New name", From: "Old name"},
		{Kind: plan.Added, Requirement: "Fresh behavior"},
	}
	if !reflect.DeepEqual(result.Capabilities[0].Operations, want) {
		t.Fatalf("operations = %#v", result.Capabilities[0].Operations)
	}
	output := string(result.Capabilities[0].Output)
	for _, absent := range []string{"Drop me", "Old name", "replaced completely"} {
		if strings.Contains(output, absent) {
			t.Fatalf("output still contains %q:\n%s", absent, output)
		}
	}
	for _, present := range []string{"### Requirement: New name", "### Requirement: Fresh behavior", "replaced by this complete block"} {
		if !strings.Contains(output, present) {
			t.Fatalf("output missing %q:\n%s", present, output)
		}
	}
}

func TestOperationsRefuseConflictsAndMissingIdentities(t *testing.T) {
	cases := []struct {
		name, accepted, delta, code string
	}{
		{
			name: "added identity exists", accepted: acceptedOne,
			delta: "## ADDED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n",
			code:  "requirement_exists",
		},
		{
			name: "modified identity missing", accepted: acceptedOne,
			delta: "## MODIFIED Requirements\n\n### Requirement: Unknown behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n",
			code:  "requirement_unknown",
		},
		{
			name: "removed identity missing", accepted: acceptedOne,
			delta: "## REMOVED Requirements\n\n### Requirement: Unknown behavior\n\n**Reason**: gone\n**Migration**: none\n",
			code:  "requirement_unknown",
		},
		{
			name: "rename source missing", accepted: acceptedOne,
			delta: "## RENAMED Requirements\n\n- FROM: `### Requirement: Unknown behavior`\n- TO: `### Requirement: Later name`\n",
			code:  "requirement_unknown",
		},
		{
			name: "rename destination exists", accepted: acceptedOne + "\n### Requirement: Later name\n\nThe system MUST stay.\n\n#### Scenario: Stay\n- **WHEN** kept\n- **THEN** kept\n",
			delta: "## RENAMED Requirements\n\n- FROM: `### Requirement: Existing behavior`\n- TO: `### Requirement: Later name`\n",
			code:  "rename_destination_exists",
		},
		{
			name: "contradictory operations", accepted: acceptedOne,
			delta: "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache more.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n\n## REMOVED Requirements\n\n### Requirement: existing behavior\n\n**Reason**: gone\n**Migration**: none\n",
			code:  "requirement_conflict",
		},
		{
			name: "partial modified block", accepted: acceptedOne,
			delta: "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache more.\n",
			code:  "requirement_scenario",
		},
		{
			name: "malformed operation heading", accepted: acceptedOne,
			delta: "## Modified Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache more.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n",
			code:  "delta_heading",
		},
		{
			name: "last requirement removed", accepted: acceptedOne,
			delta: "## REMOVED Requirements\n\n### Requirement: Existing behavior\n\n**Reason**: obsolete\n**Migration**: none\n",
			code:  "capability_empty",
		},
		{
			name:  "new capability without purpose",
			delta: "## ADDED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n",
			code:  "purpose_missing",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			owner := newOwner(t)
			if item.accepted != "" {
				writeAccepted(t, owner, "cache-policy", item.accepted)
			}
			writeDelta(t, owner, "cache-policy", item.delta)
			result := Build(owner, change)
			assertCode(t, result, item.code)
			assertWithheld(t, result)
		})
	}
}

func TestOperationsWithholdEveryCapabilityOnOneConflict(t *testing.T) {
	owner := newOwner(t)
	writeAccepted(t, owner, "cache-policy", acceptedOne)
	writeDelta(t, owner, "cache-policy", "## MODIFIED Requirements\n\n### Requirement: Existing behavior\n\nThe system MUST cache once.\n\n#### Scenario: Cache\n- **WHEN** requested\n- **THEN** cached\n")
	writeDelta(t, owner, "queue-policy", "## MODIFIED Requirements\n\n### Requirement: Missing behavior\n\nThe system MUST queue.\n\n#### Scenario: Queue\n- **WHEN** requested\n- **THEN** queued\n")
	result := Build(owner, change)
	if len(result.Capabilities) != 2 {
		t.Fatalf("capabilities = %#v", result.Capabilities)
	}
	assertWithheld(t, result)
}

func TestPreservationKeepsUnrelatedBytes(t *testing.T) {
	owner := newOwner(t)
	accepted := fixture(t, "accepted.md")
	writeAccepted(t, owner, "cache-policy", accepted)
	writeDelta(t, owner, "cache-policy", fixture(t, "delta.md"))
	result := Build(owner, change)
	if !result.Applicable {
		t.Fatalf("plan refused: %v", codes(result.Diagnostics))
	}
	output := string(result.Capabilities[0].Output)
	if output != fixture(t, "expected.md") {
		t.Fatalf("rebuilt document differs from golden:\n%s", output)
	}
	unrelated := accepted[strings.Index(accepted, "### Requirement: Keep unrelated"):strings.Index(accepted, "### Requirement: Replace me")]
	if !strings.Contains(output, unrelated) {
		t.Fatalf("unrelated bytes changed:\n%s", output)
	}
}
