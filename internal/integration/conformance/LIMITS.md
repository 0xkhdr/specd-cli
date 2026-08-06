# Runtime trace conformance limits

This is not a proof. `TestConformance` observes CLI executions in the fourteen
release journeys and checks their persisted state projections against a
hand-written model. Coverage is exactly the executions that ran.

The model is test-local and does not call production lifecycle,
task-transition, readiness, or evidence-applicability helpers. It restates the
actor, lifecycle, revision, task, dependency, and evidence rules for all 15
executable registry operations. A wrong model can still agree with a wrong
implementation; reviewing those restated rules remains an obligation.

The trace contains only operation, actor class, lifecycle, revision, task
activity, dependency identities, evidence class, refusal code, next action, and
an implementation-bug marker. It contains no file body, command output, patch,
absolute root, human identity, or Git object identity.

The observation seam is the release-journey CLI driver, not production code.
This makes observation unable to affect shipped behavior. It also means an
operation used outside that driver is unobserved until a journey invokes it.
Registry coverage requires every executable operation to have an independent
rule and an observed step; journey coverage requires all fourteen journeys;
missing or malformed result output is a coverage breach.

Four typed JSONL fixtures are byte-compared and replayed: one accepted trace
and one trace for each failure class. Refresh is explicit under
`SPECD_WRITE_CONFORMANCE_FIXTURES=1`; refresh exposes changed bytes for review
and does not establish that they are correct. A fixed-seed test judges 1,000
legal and 1,000 illegal generated model steps. Those steps exercise the checker,
not the CLI implementation.

Run:

```bash
go test ./internal/integration -run 'TestConformance|TestConformanceBites|TestConformanceFixtures|TestConformanceGeneratedSequences' -count=1
```

`TestConformanceBites` requires `IllegalTransition`, `StateMismatch`, and
`CoverageBreach` for wrong actor/lifecycle, stale evidence, incomplete
dependencies, missing operation coverage, and malformed output. The gate does
not model persistence internals, Git scope calculation, command execution,
review semantics, concurrency, or every stable refusal code; their existing
gates remain authoritative.

The next ratchet is to drive generated legal and illegal sequences through the
real CLI. Bounded exhaustive state-space enumeration remains deferred until
that smaller step finds a concrete coverage ceiling.
