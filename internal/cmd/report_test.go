package cmd

import (
	"encoding/json"
	"reflect"
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
		envelope, err := Envelope(Outcome{Operation: "report", Value: result})
		if err != nil {
			t.Fatalf("%s envelope: %v", kind, err)
		}
		text := RenderText(envelope)
		raw, err := json.Marshal(envelope.Data)
		if err != nil {
			t.Fatalf("%s json: %v", kind, err)
		}
		var decoded map[string]string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s decode: %v", kind, err)
		}
		// Both surfaces read the one envelope, so a fact can never appear in
		// one and not the other, and neither can hold a different value.
		for _, fact := range result.Facts {
			if decoded[fact.Field] != fact.Value {
				t.Fatalf("%s fact %q: json %q, model %q",
					kind, fact.Field, decoded[fact.Field], fact.Value)
			}
			if !strings.Contains(text, fact.Field+": "+fact.Value+"\n") {
				t.Fatalf("%s fact %q missing from the human surface:\n%s", kind, fact.Field, text)
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
	claims := factValue(defaulted, "maturity_claims")
	for _, claim := range core.MaturityClaims() {
		if !strings.Contains(claims, claim.Category+"/"+claim.Subject+"=") {
			t.Fatalf("maturity claims omit %s/%s: %q", claim.Category, claim.Subject, claims)
		}
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
	// Nothing is proven yet, so the review packet is not approvable and names
	// why. This is the one place that fact is projected: status does not
	// restate it.
	review, err := Report(root, change, report.KindReview, "")
	if err != nil {
		t.Fatal(err)
	}
	if factValue(review, "approvable") != "false" ||
		factValue(review, "review_blockers") == factNone {
		t.Fatalf("unproven change presented an approvable packet: %#v", review.Facts)
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
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("%s replay drifted:\n%#v\n%#v", kind, first, second)
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
