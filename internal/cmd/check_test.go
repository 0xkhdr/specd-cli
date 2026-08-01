package cmd

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestCheckHumanRender(t *testing.T) {
	result := core.CheckResult{
		Root: "/project", Change: "partial", StateRevision: 2,
		Findings: []gates.Finding{{
			Gate: gates.ProposalGate, Severity: gates.Error,
			Location: plan.Location{Path: "proposal.md", Line: 4},
			Problem:  "Problem section is empty", Repair: "add concrete content",
		}},
	}
	output := RenderCheck(result)
	for _, wanted := range []string{
		"error proposal", "proposal.md:4", "Problem section is empty",
		"repair: add concrete content", "check failed", "root /project",
	} {
		if !strings.Contains(output, wanted) {
			t.Fatalf("render missing %q: %s", wanted, output)
		}
	}
}

func TestCheckHumanRenderWarningsVisible(t *testing.T) {
	result := core.CheckResult{
		Root: "/project", Change: "warning", StateRevision: 1, Success: true,
		Findings: []gates.Finding{{
			Gate: "advice", Severity: gates.Warning,
			Problem: "review this", Repair: "inspect it",
		}},
	}
	output := RenderCheck(result)
	if !strings.Contains(output, "warning advice") || !strings.Contains(output, "review this") ||
		!strings.Contains(output, "passed with warnings") {
		t.Fatalf("warning hidden: %s", output)
	}
}
