package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/report"
)

// reportFixture is one approved change with one open attempt: enough truth for
// every report to project real lifecycle, proof, history, and review facts.
func reportFixture(t *testing.T) (root, change, taskID string) {
	t.Helper()
	change, taskID = "sample-loop", "edit-sample"
	root, _ = productionAttempt(t, change, taskID, "agent:builder")
	return root, change, taskID
}

func TestReportsRenderOnlyFourKinds(t *testing.T) {
	root, change, _ := reportFixture(t)

	for _, kind := range []string{
		report.KindStatus, report.KindProof, report.KindHistory, report.KindReview,
	} {
		result, err := Report(root, change, kind, "")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if result.Kind != kind || result.Change != change || len(result.Facts) == 0 {
			t.Fatalf("%s report = %#v", kind, result)
		}
	}

	// A fifth report needs a requirement, not a fallback. Refusal names the
	// legal recovery action rather than silently choosing a kind.
	_, err := Report(root, change, "metrics", "")
	if err == nil || !strings.Contains(err.Error(), "report kind") {
		t.Fatalf("unknown report kind = %v", err)
	}
	if _, err := Report(root, change, report.KindStatus, "dashboard"); err == nil {
		t.Fatal("an unknown profile must fail closed")
	}
}

func TestReportsHumanAndJSONAgree(t *testing.T) {
	root, change, _ := reportFixture(t)

	for _, kind := range []string{
		report.KindStatus, report.KindProof, report.KindHistory, report.KindReview,
	} {
		result, err := Report(root, change, kind, string(core.ProfileProduction))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		text := RenderReportText(result)
		raw, err := RenderReportJSON(result)
		if err != nil {
			t.Fatalf("%s json: %v", kind, err)
		}
		var decoded ReportResult
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s decode: %v", kind, err)
		}
		if len(decoded.Facts) != len(result.Facts) {
			t.Fatalf("%s: json carries %d facts, human carries %d",
				kind, len(decoded.Facts), len(result.Facts))
		}
		// Same values in the same order on both surfaces: a fact can never
		// appear in one and not the other, and order never drifts.
		lines := strings.Split(strings.TrimSpace(text), "\n")[3:]
		for index, fact := range decoded.Facts {
			if fact != result.Facts[index] {
				t.Fatalf("%s fact %d: json %#v, model %#v", kind, index, fact, result.Facts[index])
			}
			want := fact.Field + ": " + fact.Value
			if lines[index] != want {
				t.Fatalf("%s line %d = %q, want %q", kind, index, lines[index], want)
			}
		}
	}
}

func TestReportsCarryPolicyDigestAndBoundFacts(t *testing.T) {
	root, change, _ := reportFixture(t)

	defaulted, err := Report(root, change, report.KindStatus, "")
	if err != nil {
		t.Fatal(err)
	}
	production, err := Report(root, change, report.KindStatus, string(core.ProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	if factValue(defaulted, "policy_digest") == factValue(production, "policy_digest") {
		t.Fatal("two policies must not project one digest")
	}
	if factValue(production, "profile") != string(core.ProfileProduction) {
		t.Fatalf("profile fact = %q", factValue(production, "profile"))
	}
	if factValue(defaulted, "assurance") != core.AttemptAssurance {
		t.Fatalf("assurance fact = %q", factValue(defaulted, "assurance"))
	}
	// An absent value is the explicit word, never a blank row a reader could
	// mistake for an omitted fact.
	for _, fact := range defaulted.Facts {
		if strings.TrimSpace(fact.Value) == "" {
			t.Fatalf("field %q rendered an empty value", fact.Field)
		}
	}
	if got := factValue(defaulted, "friction_eligibility"); got != factNone {
		t.Fatalf("friction_eligibility = %q with no records", got)
	}
}

// A report is a projection: repeated reads produce the same facts and mutate
// nothing, which is what makes restart replay identical.
func TestReportsReplayIdentically(t *testing.T) {
	root, change, _ := reportFixture(t)

	for _, kind := range []string{
		report.KindStatus, report.KindProof, report.KindHistory, report.KindReview,
	} {
		first, err := Report(root, change, kind, "")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		second, err := Report(root, change, kind, "")
		if err != nil {
			t.Fatalf("%s replay: %v", kind, err)
		}
		if RenderReportText(first) != RenderReportText(second) {
			t.Fatalf("%s replay drifted", kind)
		}
	}
}

func factValue(result ReportResult, field string) string {
	for _, fact := range result.Facts {
		if fact.Field == field {
			return fact.Value
		}
	}
	return ""
}
