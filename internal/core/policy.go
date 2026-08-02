package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// Profile selects one built-in policy. The first release ships exactly two, so
// a flag is the whole selection surface: no config file, no schema engine.
type Profile string

const (
	ProfileDefault    Profile = "default"
	ProfileProduction Profile = "production"
)

// Policy is the one built-in policy model. Production is strictly additive: it
// adds planning rules and required proof, and can never remove, weaken, or
// reinterpret a default-profile check.
type Policy struct {
	Profile            Profile
	FullDesign         bool
	BidirectionalTrace bool
	AcceptanceReach    bool
	RequiredChecks     []evidence.RequiredCheck
}

// PolicyTransition is the explicit human record that must precede applying a
// semantically different policy to work already proven under an earlier one.
type PolicyTransition struct {
	From     string
	To       string
	Approver string
	Reason   string
}

func DefaultPolicy() Policy { return Policy{Profile: ProfileDefault} }

// ProductionPolicy is the single built-in production policy. Its required
// checks are the proof classes stage 8 adds beside the approved task
// verification command, which remains the test-run class owned by completion.
func ProductionPolicy() Policy {
	return Policy{
		Profile: ProfileProduction, FullDesign: true,
		BidirectionalTrace: true, AcceptanceReach: true,
		RequiredChecks: []evidence.RequiredCheck{
			{Class: evidence.ClassBuild, CheckID: "compile"},
			{Class: evidence.ClassLint, CheckID: "go-vet"},
			{Class: evidence.ClassReview, CheckID: "change-review"},
		},
	}
}

// ResolveProfile fails closed on any unknown selection and names the one legal
// next action together with the policy digest that is currently known.
func ResolveProfile(name string) (Policy, error) {
	switch Profile(strings.TrimSpace(name)) {
	case "", ProfileDefault:
		return DefaultPolicy(), nil
	case ProfileProduction:
		return ProductionPolicy(), nil
	}
	return Policy{}, failure.New(
		"policy_unknown", "", "",
		fmt.Sprintf("profile %q is not a known policy; current default policy digest %s",
			name, DefaultPolicyDigest()),
		"select the default or production profile",
	)
}

func (policy Policy) Production() bool { return policy.Profile == ProfileProduction }

// Validate refuses a policy whose profile and rules disagree, so production
// rules can never travel under the lean default digest.
func (policy Policy) Validate() error {
	if policy.Profile != ProfileDefault && policy.Profile != ProfileProduction {
		return failure.New("policy_unknown", "", "",
			fmt.Sprintf("profile %q is not a known policy", policy.Profile),
			"select the default or production profile")
	}
	if !policy.Production() && len(policy.Rules()) != 0 {
		return failure.New("policy_invalid", "", "",
			"the default profile carries no production rules",
			"select the production profile to apply production rules")
	}
	for _, check := range policy.RequiredChecks {
		if !check.Valid() || check.Class == evidence.ClassTestRun {
			return failure.New("policy_invalid", "", "",
				fmt.Sprintf("required check %q is not a known production check", check.String()),
				"declare build, lint, or review checks with stable check ids")
		}
	}
	return nil
}

// Rules is the normalized semantic content of a policy. Ordering is canonical,
// so two processes holding the same semantics always produce the same bytes.
func (policy Policy) Rules() []string {
	var rules []string
	if policy.FullDesign {
		rules = append(rules, "rule=full-design")
	}
	if policy.BidirectionalTrace {
		rules = append(rules, "rule=bidirectional-trace")
	}
	if policy.AcceptanceReach {
		rules = append(rules, "rule=acceptance-reach")
	}
	for _, check := range policy.RequiredChecks {
		rules = append(rules, "check="+check.String())
	}
	slices.Sort(rules)
	return rules
}

// Digest is the semantic policy identity carried by every production result and
// refusal. The default profile keeps the canonical stage-3 digest unchanged, so
// approvals and evidence recorded without production remain exactly as valid.
func (policy Policy) Digest() string {
	if !policy.Production() {
		return DefaultPolicyDigest()
	}
	normalized := "profile=production\n" + strings.Join(policy.Rules(), "\n")
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}

// CheckPolicyTransition refuses to apply a changed policy to work bound to an
// earlier digest until a human records the transition. It authorizes the new
// policy; it never substitutes for proof under it.
func CheckPolicyTransition(recorded string, current Policy, transition *PolicyTransition) error {
	target := current.Digest()
	if recorded == target {
		return nil
	}
	if transition == nil {
		return failure.New("policy_drift", "", "",
			fmt.Sprintf("recorded work is bound to policy digest %s, current policy digest is %s",
				recorded, target),
			fmt.Sprintf("record an explicit human policy transition from %s to %s", recorded, target))
	}
	if transition.From != recorded || transition.To != target ||
		strings.TrimSpace(transition.Approver) == "" || strings.TrimSpace(transition.Reason) == "" {
		return failure.New("policy_transition", "", "",
			"policy transition does not bind the recorded and current policy digests with an approver and reason",
			fmt.Sprintf("record an explicit human policy transition from %s to %s", recorded, target))
	}
	return nil
}
