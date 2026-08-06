package plan

import (
	"reflect"
	"testing"
)

func FuzzPlanSections(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("# Proposal\n\n## Problem\nNeed safety.\n\n## Outcome\nSafe.\n\n## Scope\nParser.\n\n## Non-goals\nNone.\n\n## Affected capabilities\nplanning\n"),
		[]byte("# Design\n\n## Boundaries\nParser only.\n\n## Interfaces\nNone.\n\n## Invariants\nFail closed.\n\n## Failure modes\nMalformed input.\n\n## Integration\nCore.\n\n## Alternatives\nNone.\n\n## Owner\nMaintainer.\n"),
		{}, []byte("```"), []byte{0},
	} {
		f.Add(false, seed)
		f.Add(true, seed)
	}
	f.Fuzz(func(t *testing.T, design bool, raw []byte) {
		source := Source{Path: "artifact.md", Bytes: raw, Present: true}
		if design {
			first := ParseDesign(source)
			second := ParseDesign(source)
			assertFuzzDiagnostics(t, first.Diagnostics)
			if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first.Source.Bytes, raw) {
				t.Fatal("design parse is unstable or lost source bytes")
			}
			return
		}
		first := ParseProposal(source)
		second := ParseProposal(source)
		assertFuzzDiagnostics(t, first.Diagnostics)
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first.Source.Bytes, raw) {
			t.Fatal("proposal parse is unstable or lost source bytes")
		}
	})
}

func FuzzParseTasks(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("# Tasks\n\n| id | role | files | depends-on | refs | verify | acceptance |\n|---|---|---|---|---|---|---|\n| T1 | builder | sample.go | | sample/Requirement: Safe | go test ./... | passes |\n"),
		{}, []byte("|"), []byte("```"), []byte{0},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		source := Source{Path: "tasks.md", Bytes: raw, Present: true}
		first := ParseTasks(source)
		second := ParseTasks(source)
		assertFuzzDiagnostics(t, first.Diagnostics)
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first.Source.Bytes, raw) {
			t.Fatal("task parse is unstable or lost source bytes")
		}
	})
}

func FuzzParseCapabilityDelta(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("# Sample\n\n## Purpose\nSafety.\n\n## ADDED Requirements\n### Requirement: Safe\nThe system MUST be safe.\n\n#### Scenario: Works\n- **WHEN** used\n- **THEN** it works\n"),
		{}, []byte("## ADDED Requirements"), []byte("```"), []byte{0},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		source := Source{Path: "spec.md", Bytes: raw, Present: true}
		first := ParseCapabilityDelta(source, "sample", Source{})
		second := ParseCapabilityDelta(source, "sample", Source{})
		assertFuzzDiagnostics(t, first.Diagnostics)
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first.Source.Bytes, raw) {
			t.Fatal("delta parse is unstable or lost source bytes")
		}
	})
}

func assertFuzzDiagnostics(t *testing.T, diagnostics []Diagnostic) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "" || diagnostic.Message == "" || diagnostic.Repair == "" {
			t.Fatalf("non-actionable diagnostic: %#v", diagnostic)
		}
	}
}
