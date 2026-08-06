package core

import "slices"

// Maturity and Assurance are published claim levels. Maturity describes what
// has been observed; assurance describes whether the host contains a process.
type Maturity string
type Assurance string

const (
	MaturityProven       Maturity = "proven"
	MaturityGated        Maturity = "gated"
	MaturityExperimental Maturity = "experimental"
	MaturityUnclaimed    Maturity = "unclaimed"

	AssuranceAdvisory Assurance = "advisory"
)

// MaturityClaim is one published platform, profile, guarantee, or coverage
// claim. Exactly one of Maturity and Assurance is set.
type MaturityClaim struct {
	Category  string
	Subject   string
	Maturity  Maturity
	Assurance Assurance
	Observed  string
	Evidence  string
}

var maturityClaims = []MaturityClaim{
	{Category: "platform", Subject: "linux/amd64", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "release/release-decision.md#supported-platforms"},
	{Category: "platform", Subject: "linux/arm64", Maturity: MaturityGated, Observed: "2026-08-01", Evidence: "release/release-decision.md#supported-platforms"},
	{Category: "platform", Subject: "darwin/arm64", Maturity: MaturityGated, Observed: "2026-08-01", Evidence: "release/release-decision.md#supported-platforms"},
	{Category: "platform", Subject: "windows/amd64", Maturity: MaturityGated, Observed: "2026-08-01", Evidence: "release/release-decision.md#supported-platforms"},
	{Category: "profile", Subject: "default", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "release/release-decision.md#implemented-base-loop"},
	{Category: "profile", Subject: "production", Maturity: MaturityExperimental, Observed: "2026-08-01", Evidence: "release/release-decision.md#stage-8-status"},
	{Category: "guarantee", Subject: "approval", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "evidence", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "scope", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "atomicity", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "fail-closed", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "path-containment", Maturity: MaturityProven, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-defends"},
	{Category: "guarantee", Subject: "host-assurance", Assurance: AssuranceAdvisory, Observed: "2026-08-01", Evidence: "SECURITY.md#what-specd-does-not-defend"},
	{Category: "coverage", Subject: "concurrent-end-to-end", Maturity: MaturityUnclaimed, Observed: "2026-08-01", Evidence: "release/release-decision.md#known-limitations"},
}

// MaturityClaims returns the registry in stable declaration order. Callers get
// a copy so a projection cannot mutate published truth.
func MaturityClaims() []MaturityClaim { return slices.Clone(maturityClaims) }
