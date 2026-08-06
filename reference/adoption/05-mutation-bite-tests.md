# 05 — Prove the gates bite

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P3](../patterns.md#p3--a-gate-must-be-provable-and-its-blind-spots-must-be-written-down) | 2 | medium | low | not applied |

## Why

buzz's `LIMITS.md` states the discipline in one sentence: "every `TraceAction`
variant has a passing case and at least one **mutation-class bite case**." And
it explains why the sentence exists: "Coverage breach is load-bearing. Without
it, trace conformance is decorative logging."

specd has 83 test files and nine mechanically asserted release gates. What no
test currently establishes is that **any of those gates would fail if the thing
it protects were broken.** A gate that cannot be made to fail is indistinguishable
from a gate that does nothing, and specd's whole value proposition is that its
refusals are real.

This is the highest-leverage item in the whole reference set, because it costs
almost nothing and it is the one that decides whether every other gate is
meaningful.

## The rule to adopt

> Every enforcement point in specd has, in the same package, at least one test
> that constructs the state the enforcement forbids and asserts the exact
> refusal — not merely that an error occurred, but the refusal code and the one
> legal next action it names.

"Enforcement point" means each of the eight foundation invariants named in
`release/surface-inventory.md`: validation, approval, authority, scope,
evidence, staleness, atomicity, fail-closed.

## Change set

### 5.1 Audit what already bites

Most of this probably exists. Do not write new tests before establishing what
is covered. Produce `reference/bite-audit.md` (a working file, deleted when the
item is applied) with one row per enforcement point:

| invariant | enforcement site | bite test | refusal code asserted |
| --- | --- | --- | --- |
| approval | `internal/core/approval.go`, `approval_status.go` | ? | ? |
| authority | `internal/core/tasktransition.go` — the sealed `TaskTransitionAuthority` | ? | ? |
| scope | declared-file validation in `internal/core` | ? | ? |
| evidence | `internal/core/evidence`, `complete.go` | ? | ? |
| staleness | revision guards, HEAD-bound evidence | ? | ? |
| atomicity | `internal/core/persist`, `transaction` | ? | ? |
| validation | `internal/core/gates` | ? | ? |
| fail-closed | `internal/core/failure`, `state` decode | ? | ? |

Fill it by reading the existing tests. Rows that come back empty, or that assert
only `err != nil`, are the work.

### 5.2 Strengthen weak assertions

The common weakness in a mature Go suite is `if err == nil { t.Fatal(...) }`.
specd's contract is stronger than "an error": every refusal "returns one legal
next action" (`README.md` §Guarantees). Assert that.

```go
// Weak: passes even if the refusal is the wrong one, or names no next action.
if err == nil {
	t.Fatal("expected refusal")
}

// Bite: fails if the refusal changes shape, changes code, or stops naming
// exactly one legal next action — which is the contract README publishes.
var refusal *failure.Failure // use the concrete type from internal/core/failure
if !errors.As(err, &refusal) {
	t.Fatalf("refusal is not the published failure shape: %v", err)
}
if got, want := refusal.Code, "task_unauthorized"; got != want {
	t.Fatalf("refusal code = %q, want %q", got, want)
}
if refusal.NextAction == "" {
	t.Fatal("refusal names no legal next action")
}
```

Adjust field and constructor names to the real `internal/core/failure` API. The
point is the three assertions, not the syntax.

### 5.3 Add the missing bite cases

For every empty row, write one test that constructs the forbidden state
directly — not through the happy path with a flag flipped, which usually proves
only that the flag works. Examples of the state to construct:

- **authority** — a `TaskTransitionRequest` whose `Authority` is the zero value.
  `trustedTaskTransitionAuthority` is unexported precisely so a caller cannot
  forge one; the test proves the zero value is refused rather than treated as an
  empty-but-valid actor.
- **staleness** — evidence recorded at one HEAD, then a moved HEAD, then
  `complete`. Assert the refusal, and assert that no completion, no history
  entry, and no state revision bump occurred.
- **atomicity** — an injected write failure mid-transaction. `journey:10`
  already covers this; the bite assertion is that the durable bytes are *exactly*
  old or *exactly* new, byte-compared, with nothing in between.
- **fail-closed** — a `state.json` with a future schema version, a truncated
  `history.jsonl`, and a `state.json` with valid JSON but a semantically
  impossible lifecycle. Each must refuse, and none may be silently repaired.

### 5.4 Record the discipline

Add to `AGENTS.md` §Design rules, in the register that file already uses:

> - Every enforcement point carries a bite test: a case that constructs the
>   forbidden state and asserts the refusal code and its one legal next action.
>   A gate no test can make fail is not a gate. Weakening a bite test to make a
>   change pass is never the fix.

That last sentence is the pre-refused escape hatch, in the same shape as buzz's
"never bump the limit or add an override to slip under it."

## Acceptance

Every row in the audit table is filled with a named test and an asserted refusal
code. Then, for three enforcement points chosen at random, verify by mutation:

```bash
# Temporarily invert a guard in the production path, e.g. change a `!=` to `==`
# in the revision check. Then:
go test ./... -count=1
# The corresponding bite test MUST fail. Revert.
```

If the suite stays green with the guard inverted, the bite test is not biting
and the row is not done. Record the three mutations you tried and their results
in the acceptance note — buzz's `LIMITS.md` states its exact test counts for the
same reason.

## Do not

- Do not add a mutation-testing framework. Manual, recorded mutation of three
  guards is enough evidence for a project this size, and a framework would be a
  dependency.
- Do not assert on refusal *message text*. Assert on the code and the next
  action. Message wording is presentation, and the one output owner rule means
  presentation lives in `internal/cmd/output.go`, not in a core test.
- Do not weaken a bite test to make an unrelated change pass. If a bite test
  fails, either the change broke an invariant or the invariant genuinely moved —
  and a moved invariant is a `release/release-decision.md` edit, not a test edit.

## Deferred

Coverage percentage targets. Line coverage would say nothing here: the question
is whether the *refusals* fire, and a bite test answers that directly while a
coverage number does not.
