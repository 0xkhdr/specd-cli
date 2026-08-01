package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestProductionPolicySelectionAndDefaultLeanness(t *testing.T) {
	for _, name := range []string{"", "default"} {
		policy, err := ResolveProfile(name)
		if err != nil || policy.Production() || len(policy.RequiredChecks) != 0 ||
			len(policy.Rules()) != 0 || policy.Digest() != DefaultPolicyDigest() {
			t.Fatalf("default %q = %#v, %v", name, policy, err)
		}
	}
	production, err := ResolveProfile("production")
	if err != nil || !production.Production() || len(production.RequiredChecks) == 0 {
		t.Fatalf("production = %#v, %v", production, err)
	}
	if err := production.Validate(); err != nil {
		t.Fatalf("built-in production policy is invalid: %v", err)
	}
	for _, unknown := range []string{"prod", "PRODUCTION", "strict", "default-plus"} {
		policy, err := ResolveProfile(unknown)
		if !failure.IsCode(err, "policy_unknown") || policy.Production() {
			t.Fatalf("unknown profile %q = %#v, %v", unknown, policy, err)
		}
		var refusal *failure.Refusal
		if !errors.As(err, &refusal) || refusal.Next == "" ||
			!strings.Contains(refusal.Reason, DefaultPolicyDigest()) {
			t.Fatalf("refusal = %#v", err)
		}
	}
	// A default policy carrying production rules is a contradiction, not a
	// quiet upgrade under the lean digest.
	sneaky := DefaultPolicy()
	sneaky.FullDesign = true
	if err := sneaky.Validate(); !failure.IsCode(err, "policy_invalid") {
		t.Fatalf("default policy with production rules = %v", err)
	}
	unknownCheck := ProductionPolicy()
	unknownCheck.RequiredChecks = append(unknownCheck.RequiredChecks,
		evidence.RequiredCheck{Class: "scan", CheckID: "secrets"})
	if err := unknownCheck.Validate(); !failure.IsCode(err, "policy_invalid") {
		t.Fatalf("unknown class = %v", err)
	}
}

func TestProductionPolicyDigestStableAndSemantic(t *testing.T) {
	first, second := ProductionPolicy(), ProductionPolicy()
	if first.Digest() != second.Digest() || first.Digest() == DefaultPolicyDigest() {
		t.Fatalf("digest = %q / %q", first.Digest(), second.Digest())
	}
	// Representation, not semantics: reordering declarations must not move the
	// digest, because the normalized rule set is identical.
	reordered := ProductionPolicy()
	checks := reordered.RequiredChecks
	reordered.RequiredChecks = []evidence.RequiredCheck{checks[2], checks[0], checks[1]}
	if reordered.Digest() != first.Digest() {
		t.Fatalf("reordered digest = %q, want %q", reordered.Digest(), first.Digest())
	}
	for _, changed := range []Policy{
		func() Policy { p := ProductionPolicy(); p.AcceptanceReach = false; return p }(),
		func() Policy { p := ProductionPolicy(); p.FullDesign = false; return p }(),
		func() Policy {
			p := ProductionPolicy()
			p.RequiredChecks[1].CheckID = "golangci"
			return p
		}(),
		func() Policy {
			p := ProductionPolicy()
			p.RequiredChecks = p.RequiredChecks[:2]
			return p
		}(),
	} {
		if changed.Digest() == first.Digest() {
			t.Fatalf("semantic change kept digest %q: %#v", changed.Digest(), changed)
		}
	}
}

func TestProductionPolicyTransitionIsExplicit(t *testing.T) {
	current := ProductionPolicy()
	if err := CheckPolicyTransition(current.Digest(), current, nil); err != nil {
		t.Fatalf("same policy = %v", err)
	}
	prior := ProductionPolicy()
	prior.AcceptanceReach = false
	if err := CheckPolicyTransition(prior.Digest(), current, nil); !failure.IsCode(err, "policy_drift") {
		t.Fatalf("drift = %v", err)
	}
	for _, transition := range []PolicyTransition{
		{From: prior.Digest(), To: current.Digest(), Approver: "human@example.com"},
		{From: prior.Digest(), To: current.Digest(), Reason: "adopt production"},
		{From: current.Digest(), To: current.Digest(), Approver: "human@example.com", Reason: "adopt"},
		{From: prior.Digest(), To: prior.Digest(), Approver: "human@example.com", Reason: "adopt"},
	} {
		if err := CheckPolicyTransition(prior.Digest(), current, &transition); !failure.IsCode(err, "policy_transition") {
			t.Fatalf("incomplete transition %#v = %v", transition, err)
		}
	}
	valid := PolicyTransition{
		From: prior.Digest(), To: current.Digest(),
		Approver: "human@example.com", Reason: "adopt production policy",
	}
	if err := CheckPolicyTransition(prior.Digest(), current, &valid); err != nil {
		t.Fatalf("valid transition = %v", err)
	}
}
