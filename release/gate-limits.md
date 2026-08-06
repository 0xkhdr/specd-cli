# Gate limits

[`release-decision.md`](release-decision.md) records whether each gate is
green. This file records what a green gate does not establish. Implementation
wins over both documents when they disagree, and both documents then need a
fix.

## standard-library-only default binary

Establishes: `go.mod` has no required module at test time.
Does not establish: alternate build tags add none, local subprocesses are
absent, or linked standard-library code has no advisory.

## formatting clean

Establishes: current Go files equal `gofmt` output.
Does not establish: generated non-Go files are current or code is correct.

## generated docs parity

Establishes: `docs/operations.md` equals registry projection byte-for-byte.
Does not establish: registry usage text matches command behavior. Wrong input
is rendered faithfully.

## no broken link in the user documentation

Establishes: relative inline links in `README.md` and `docs/*.md` resolve in
this checkout.
Does not establish: anchors exist, external URLs work, or links elsewhere are
valid.

## generated guidance parity

Establishes: guidance renders deterministically and names every registered,
agent-visible executable operation.
Does not establish: operation prose is correct or every named operation is
reachable through dispatch.

## maturity claims complete and consistent

Establishes: the typed registry has every required claim, each row has a level,
date, and resolving evidence path, and the primary user and security summaries
state matching levels.
Does not establish: the cited evidence proves its claim, anchors resolve, an
unscanned document agrees, or a dated observation remains current forever.

## all fourteen required journeys retained

Establishes: runner contains fourteen journeys whose names overlap the required
list.
Does not establish: a journey still asserts its named behavior. A gutted
journey can retain its name.

## runtime trace conformance

Establishes: executions in all fourteen journeys emitted bounded state steps,
every executable operation was reached and has an independent rule, malformed
result output fails closed, generated model steps exercise legal and illegal
cases, and four committed trace fixtures replay byte-for-byte. Full limits are
in `internal/integration/conformance/LIMITS.md`.
Does not establish: production decision-seam tracing, generated sequences
through the real CLI, exhaustive states, correctness of the hand-written model,
every refusal code, persistence internals, concurrency, or behavior outside the
journeys that ran.

## no unowned surface

Establishes: live inventoried surface has an owner row and dead rows are
refused.
Does not establish: owner text is correct or its journey reaches the surface.
Ownership remains reviewable evidence, not re-derived proof.

## no dead vocabulary in the user and agent surface

Establishes: guidance template, generated operations, and registry help omit
forbidden nouns.
Does not establish: every authored document, source comment, or runtime refusal
body omits them.

## no network or LLM path in the deterministic core

Establishes: listed deterministic packages do not import listed network APIs.
Does not establish: future packages join the list, indirect local subprocesses
cannot use a network, or imports outside the list are clean.

## gate limits complete

Establishes: every gate row in the release decision has an exact limits
heading.
Does not establish: limits prose is accurate or complete. Implementation and
review remain authoritative.

## go test ./... -race -count=1

Establishes: suite and race detector passed for exercised paths on recorded
runners.
Does not establish: absence of races outside those paths or a hand-driven loop
on platforms not named as supported.

## go vet ./...

Establishes: configured `go vet` analyzers found no issue in current packages.
Does not establish: broader static analysis, runtime correctness, or security.

## release contract self-checks

Establishes: tag and preparation contracts accept and reject their synthetic
cases on Ubuntu, and preparation creates a branch and commit but no tag.
Does not establish: a merged PR creates a tag, publication succeeds, or other
shells behave identically.

## no advisory reachable from called code (`govulncheck`)

Establishes: current advisory database found no known vulnerability reachable
from analyzed calls.
Does not establish: no unknown vulnerability exists, specd logic is correct,
or tomorrow's database returns the same result.

## bounded fuzz burst

Establishes: three high-risk targets explored generated inputs for 60 seconds
on one Ubuntu runner.
Does not establish: exhaustive inputs, deterministic discoveries, other
platform behavior, or coverage of every fuzz target beyond committed seeds.

## growth benchmarks compile and run

Establishes: the four benchmark families compile and complete once without a
panic on the runner. Dated manual measurements live in `release/scale.md`.
Does not establish: a timing threshold, supported maximum, another machine's
behavior, concurrency under load, or long-lived-change behavior.

## Cross-cutting limits

- Gates observe a checkout, not a deployment or host containment.
- Bite tests prove named forbidden examples fail. Native fuzzing broadens input
  samples; neither proves absence.
- Golden fixtures expose byte changes for review. Refresh can bless bad bytes.
- CI fuzz bursts run on Ubuntu only, target one parser at a time, and are
  bounded observations.

## Next ratchet

Largest conformance blind spot: generated sequences exercise the independent
checker but not the CLI. Drive bounded generated legal and illegal sequences
through the existing journey driver before considering exhaustive state-space
enumeration.
