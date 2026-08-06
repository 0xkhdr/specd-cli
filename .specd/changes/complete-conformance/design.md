# Design

## Boundaries
release-assurance/Requirement: Runtime conformance coverage
Conformance remains test-local and observes the existing release-journey CLI
driver. Production packages expose no new tracing surface and tracing cannot
influence a command decision.

## Interfaces
`conformanceStep` remains the bounded trace record. `checkConformance` judges
steps against a hand-written operation table. Tests reconstruct committed
JSONL fixtures and refresh them only when
`SPECD_WRITE_CONFORMANCE_FIXTURES=1`.

## Invariants
The model does not call production lifecycle, task-transition, readiness, or
evidence-applicability helpers. Every executable registry operation and every
retained journey is covered. Refusals preserve modeled state and carry one
next action. Successful operations obey their actor, lifecycle, revision, task,
dependency, and evidence rules. Missing or malformed result envelopes fail as
implementation bugs rather than becoming weak steps.

## Failure behavior
Illegal operation or lifecycle transitions report `IllegalTransition`; wrong
state projections report `StateMismatch`; missing envelopes, operations,
journeys, or required classes report `CoverageBreach`. Fixture drift reports
the exact opt-in refresh command.

## Integration
Extend `internal/integration/conformance_test.go` and its existing release
journey hooks. Keep limits in `internal/integration/conformance/LIMITS.md` and
the release-facing summary in `release/gate-limits.md`. Record the exact armed
boundary and acceptance evidence in the existing adoption and release files.

## Alternatives
A production callback or exported trace package adds shipped surface for a
test-only observation. Full state-space enumeration is deferred until generated
sequences expose a concrete coverage ceiling.

## Owner
`internal/integration` owns the observer, independent model, fixtures, and
checker; `release/` owns the published claim and its limits.
