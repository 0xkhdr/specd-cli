# Design

## Boundaries
release-assurance/Architecture documentation: Repository documentation explains
only implemented code shape and links to ownership and release evidence.

release-assurance/Canonical maturity claims: A deterministic core registry owns
published platform, profile, and guarantee claims; existing report and guidance
surfaces project it.

release-assurance/Contributor contract: Contributor documentation states only
rules with an existing or newly added mechanical enforcement point.

## Interfaces
`core.MaturityClaims` returns a defensive copy in stable declaration order.
`generate.Render` receives those claims as template data. `report --kind status`
adds bounded claim facts through `cmd.ReportResult` and the existing output
owner. `TestReleaseQualification` validates claim completeness, evidence, and
document parity.

## Invariants
One authored claim registry, one report envelope, one output owner, one
validation boundary, deterministic local projection, no runtime dependency,
and no change to approval, evidence, authority, scope, or completion.

## Failure behavior
An invalid, incomplete, unsupported, or contradictory claim makes release
qualification fail. A broken relative link in `ARCHITECTURE.md` fails the
existing documentation-link gate. Report projection remains read-only.

## Integration
Reuse `internal/core`, `internal/generate`, `internal/cmd/report.go`,
`internal/cmd/output.go`, and `internal/integration/release_test.go`. Extend the
existing surface inventory and release gate/limits records; add no parallel
renderer or parser.

## Alternatives
A JSON registry adds a parser and schema for one Go consumer. A fifth report
kind duplicates the existing status projection. DCO is omitted because the
repository has no required sign-off check and Phase 4 forbids unenforced rules.

## Owner
`internal/core` owns claim truth; `internal/generate` and `internal/cmd` project
it; `internal/integration` enforces repository parity.
