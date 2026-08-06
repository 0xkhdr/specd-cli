package cmd

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

// renderCheck projects one check result through the shipped human surface.
// There is no second renderer to test: the terminal reads the same envelope
// the JSON document does.
func renderCheck(t *testing.T, result core.CheckResult) string {
	t.Helper()
	exit := 0
	if !result.Success {
		exit = 1
	}
	envelope, err := Envelope(Outcome{Operation: "check", Value: result, Exit: exit})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	return RenderText(envelope)
}

func TestCheckHumanRender(t *testing.T) {
	output := renderCheck(t, core.CheckResult{
		Root: "/project", Change: "partial", StateRevision: 2,
		Findings: []gates.Finding{{
			Gate: gates.ProposalGate, Severity: gates.Error,
			Location: plan.Location{Path: "proposal.md", Line: 4},
			Problem:  "Problem section is empty", Repair: "add concrete content",
		}},
	})
	for _, wanted := range []string{
		"error proposal:", "proposal.md:4", "Problem section is empty",
		"fix: add concrete content", "root: /project", "change: partial",
		"revision: 2", "exit: 1",
	} {
		if !strings.Contains(output, wanted) {
			t.Fatalf("render missing %q: %s", wanted, output)
		}
	}
}

func TestCheckHumanRenderWarningsVisible(t *testing.T) {
	output := renderCheck(t, core.CheckResult{
		Root: "/project", Change: "warning", StateRevision: 1, Success: true,
		Findings: []gates.Finding{{
			Gate: "advice", Severity: gates.Warning,
			Problem: "review this", Repair: "inspect it",
		}},
	})
	if !strings.Contains(output, "warning advice:") || !strings.Contains(output, "review this") ||
		!strings.Contains(output, "exit: 0") {
		t.Fatalf("warning hidden: %s", output)
	}
}
