package plan

import (
	"bytes"
	"reflect"
	"testing"
)

func TestSectionsProposalPreservesSourceAndLocations(t *testing.T) {
	raw := []byte("# Proposal\r\n\r\n## Problem\r\nNeed safer plans.\r\n\r\n## Outcome\r\nPlans parse.\r\n\r\n## Scope\r\nParser only.\r\n\r\n## Non-goals\r\nNo writes.\r\n\r\n## Affected capabilities\r\nplanning\r\n")
	proposal := ParseProposal(Source{Path: "proposal.md", Bytes: raw, Present: true})
	if !bytes.Equal(proposal.Source.Bytes, raw) {
		t.Fatal("source bytes changed")
	}
	if len(proposal.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", proposal.Diagnostics)
	}
	if len(proposal.Sections) != 5 || proposal.Sections[0].Location.Line != 3 ||
		string(proposal.Sections[0].Content) != "Need safer plans.\r\n\r\n" {
		t.Fatalf("unexpected sections: %#v", proposal.Sections)
	}
}

func TestSectionsPartialMalformedAndPlaceholder(t *testing.T) {
	raw := []byte("## Problem\nConcrete.\n\n### Outcome\nWrong level.\n\n## Scope\n<scope>\n")
	proposal := ParseProposal(Source{Path: "proposal.md", Bytes: raw, Present: true})
	if got := sectionNames(proposal.Sections); !reflect.DeepEqual(got, []string{"Problem", "Scope"}) {
		t.Fatalf("recognized sections = %#v", got)
	}
	assertCodes(t, proposal.Diagnostics,
		"section_level", "section_placeholder", "section_missing", "section_missing")
}

func TestSectionsFenceAndInterruptedInput(t *testing.T) {
	raw := []byte("## Boundaries\nConcrete.\n\n````md\n## Interfaces\nfake\n```\n## Owner\nalso fenced\n")
	design := ParseDesign(Source{Path: "design.md", Bytes: raw, Present: true})
	if got := sectionNames(design.Sections); !reflect.DeepEqual(got, []string{"Boundaries"}) {
		t.Fatalf("fenced headings parsed: %#v", got)
	}
	found := false
	for _, item := range design.Diagnostics {
		if item.Code == "fence_unterminated" && item.Location.Line == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing interrupted-fence diagnostic: %#v", design.Diagnostics)
	}
}

func TestSectionsDesignReferences(t *testing.T) {
	raw := []byte("## Boundaries\napi/Requirement: Stable parser\n```\nfake/Requirement: Fenced prose\n```\n## Interfaces\nValues only.\n## Invariants\nBytes stay.\n## Failure behavior\nFail closed.\n## Integration\nplan/Requirement: Shared model\n## Alternatives\nNo sibling parser.\n## Owner\ninternal/plan\n")
	design := ParseDesign(Source{Path: "design.md", Bytes: raw, Present: true})
	if len(design.Diagnostics) != 0 || len(design.References) != 2 {
		t.Fatalf("unexpected design: %#v", design)
	}
	if design.References[0].Capability != "api" ||
		design.References[0].Requirement != "Stable parser" ||
		design.References[0].Location.Line != 2 {
		t.Fatalf("unexpected reference: %#v", design.References[0])
	}
}

func TestSectionsDesignRejectsMalformedReferences(t *testing.T) {
	raw := []byte("## Boundaries\nAccounts/Requirement: Stable parser\n## Interfaces\nValues only.\n## Invariants\nBytes stay.\n## Failure behavior\nFail closed.\n## Integration\napi/Requirement:\n## Alternatives\nNo sibling parser.\n## Owner\ninternal/plan\n")
	design := ParseDesign(Source{Path: "design.md", Bytes: raw, Present: true})
	if len(design.References) != 0 || countCode(design.Diagnostics, "reference_syntax") != 2 {
		t.Fatalf("malformed references trusted: %#v", design)
	}
}

func TestSectionsMissingArtifact(t *testing.T) {
	proposal := ParseProposal(Source{Path: "proposal.md"})
	assertCodes(t, proposal.Diagnostics, "artifact_missing")
}

func TestSectionsLFCRLFParity(t *testing.T) {
	lf := []byte("## Problem\n<x>\n## Outcome\n\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	a := ParseProposal(Source{Path: "proposal.md", Bytes: lf, Present: true})
	b := ParseProposal(Source{Path: "proposal.md", Bytes: crlf, Present: true})
	if !reflect.DeepEqual(codes(a.Diagnostics), codes(b.Diagnostics)) {
		t.Fatalf("diagnostic drift: %v != %v", codes(a.Diagnostics), codes(b.Diagnostics))
	}
	if bytes.Equal(a.Source.Bytes, b.Source.Bytes) {
		t.Fatal("original line endings not preserved")
	}
}

func sectionNames(sections []Section) []string {
	out := make([]string, len(sections))
	for i, section := range sections {
		out[i] = section.Name
	}
	return out
}

func assertCodes(t *testing.T, diagnostics []Diagnostic, want ...string) {
	t.Helper()
	got := codes(diagnostics)
	for _, code := range want {
		found := false
		for i, candidate := range got {
			if candidate == code {
				got = append(got[:i], got[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s in %#v", code, diagnostics)
		}
	}
	if len(got) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func codes(diagnostics []Diagnostic) []string {
	out := make([]string, len(diagnostics))
	for i, item := range diagnostics {
		out[i] = item.Code
	}
	return out
}
