# Runtime trace conformance limits

This is not a proof. `TestConformance` observes the CLI executions in the
fourteen release journeys and checks their persisted state projections against
a hand-written model. Coverage is exactly the executions that ran.

The model is test-local and does not call production lifecycle, task-transition,
readiness, or evidence-applicability helpers. A wrong model can still agree with
a wrong implementation; reviewing the restated rules is part of reviewing this
gate. The trace contains only operation, actor class, lifecycle, revision, task
activity, dependency identities, evidence class, refusal code, and next action.
It contains no file body, command output, patch, absolute root, human identity,
or Git object identity.

The observation seam is the release-journey CLI driver, not production code.
This keeps the tracer unable to affect shipped behavior, but an operation used
only outside that driver is visible only through registry coverage and remains
unobserved until a journey invokes it. Every executable registry operation and
each of the fourteen journeys must emit at least one step.

The checker covers lifecycle and task transitions, human routing for approval
and sync, monotonic exact revisions, refusal immutability with one next action,
applicable passing evidence before completion, and completed dependencies. It
does not model persistence internals, Git scope calculation, command execution,
review semantics, or concurrency; their existing gates remain authoritative.

Run `go test ./internal/integration -run TestConformance -count=1`.
`TestConformanceBites` constructs illegal-transition, state-mismatch,
coverage-breach, and incomplete-dependency traces and requires each named
failure. The next ratchet is generated legal and illegal CLI sequences; bounded
state-space enumeration remains deferred.
