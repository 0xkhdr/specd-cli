# 09 — Model conformance: an independent judge of the lifecycle

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P4](../patterns.md#p4--the-checker-must-be-independent-of-the-thing-it-checks), [P5](../patterns.md#p5--coverage-breach-fails-closed) | 3 | large | medium | applied 2026-08-06 |

This is the largest item in the reference set and the one with the highest
ceiling. Do not start it before [05](05-mutation-bite-tests.md),
[06](06-fuzz-and-property-tests.md), and [07](07-golden-fixture-contract.md) are
applied — it depends on the transition table and the fixture conventions those
items establish.

## Why

buzz's `crates/buzz-conformance` states the north star:

> Don't ask "did the model pass"; ask "did the running code emit a trace the
> model accepts."

specd is a better fit for this than buzz is. buzz had to model a distributed,
multi-tenant relay. specd's subject is a **deterministic, local, single-process
state machine** — lifecycle states, task activities, approval status, readiness,
evidence applicability. That is a small model with a finite transition relation
that can be written down completely.

What specd's suite proves today: fourteen example journeys reach the expected
end states, and a set of refusals fire on constructed bad inputs. What it does
not prove: that **no reachable sequence of operations** produces an illegal
transition, and that a newly added operation is exercised by anything at all. A
journey gutted to a no-op keeps its name and passes the retention gate —
[adoption 08](08-limits-docs.md) records that blind spot; this item closes it.

## The four rules that make this real

Take these from buzz verbatim in spirit. Violating any one of them produces a
tautology that costs effort and proves nothing.

### Rule 1 — the model is independent

The model **restates** the legal transition relation in test-local code. It does
**not** import `core.TransitionTaskActivity`, the approval-status helpers, or
the readiness projection. buzz's reasoning applies exactly:

> Sharing normalization helpers between the emitter (which projects
> implementation state) and the checker (which judges that projection) would let
> a bug in the helpers hide itself from both.

If the model imports the production transition function, a bug in that function
is legal by definition and the whole item is decorative.

### Rule 2 — the trace is a projection, not a dump

A trace step carries the minimum needed to judge a transition:

```go
// internal/integration/conformance/trace.go (package conformance, test-support)
type Step struct {
	Schema    int    `json:"schema"`
	Operation string `json:"operation"` // registry name: init, new, start, verify, …
	Actor     string `json:"actor"`     // "human" | "agent" — the routed class, not an identity
	Before    Model  `json:"before"`
	After     Model  `json:"after"`
}

// Model is the abstract state, deliberately much smaller than state.State.
type Model struct {
	Lifecycle  string            `json:"lifecycle"`
	Approved   bool              `json:"approved"`
	Revision   uint64            `json:"revision"`
	Tasks      map[string]string `json:"tasks"`     // task id -> activity
	Evidence   map[string]string `json:"evidence"`  // task id -> applicable | stale | none
	Refusal    string            `json:"refusal"`   // refusal code, empty on success
	NextAction string            `json:"next_action"`
}
```

What it must **not** carry: file contents, command output, patches, absolute
paths, approver or reviewer identities, or Git object contents. That constraint
already exists in specd's published contract — the managed `AGENTS.md` block
says reports carry "bounded identities, counts, and codes — never source bodies,
command output, patches, or logs." The trace obeys the same rule, for the same
reason, and saying so in the trace file's doc comment keeps the two aligned.

### Rule 3 — coverage breach fails closed

This is what separates a gate from logging. Two breaches must fail:

- **Unemitted seam.** An operation runs to a decision without emitting a step.
  Implement with the Go equivalent of buzz's `EmitGuard`: arm a guard at
  operation entry, and in a deferred call record `ImplBug` if nothing was
  emitted. Assert zero `ImplBug` steps at the end of every run.
- **Unexercised operation.** Every operation in the registry must appear in at
  least one trace across the conformance run. A newly registered operation that
  no journey drives fails the gate rather than passing silently.

The second is the specd-specific one, and it is the reason to do this item at
all. It is the same reflex as `subtraction_test.go` — surface without an
exerciser fails — extended from symbols to behavior.

Be honest about the residual hole, as buzz is: a *new* operation that never arms
a guard is invisible to the harness. buzz says "that's enforced by code review,
not by the harness." specd can do better, because its operations come from one
registry: derive the required-operation list from the registry itself, so
registering an operation is what puts it on the list.

### Rule 4 — observation only

The tracer never influences a decision. Production builds carry a no-op tracer;
the conformance test injects a recording one. Turning it off loses observability
and nothing else. Wire it as an unexported package-level hook in `internal/core`
that defaults to nil, or as a field on an existing context/request struct —
whichever costs fewer touched call sites.

## Change set

### 9.1 The model — `internal/integration/conformance/model.go`

Hand-written, test-support only, importing nothing from `internal/core`:

- the set of lifecycle states and the legal transitions between them;
- the set of task activities and their legal transitions;
- the operations legal from each state, and for each, the required actor class;
- the invariants, as predicates over `Model`:
  - `complete` requires applicable, non-stale evidence for that task;
  - approval and sync require `actor == "human"`;
  - revision is non-decreasing, and increases on exactly the state-writing
    operations;
  - a step with a non-empty `Refusal` leaves `Before` and `After` equal except
    for `Refusal`/`NextAction`, and names exactly one next action;
  - no task reaches `completed` while its dependencies are not `completed`.

### 9.2 The emitter

At the decision point in each operation — the same place the result envelope is
built — emit one `Step`. There is exactly one such place per operation if the
one-output-owner rule is holding, which makes this cheap. If an operation has
more than one, that is a finding for `internal/cmd/output.go`, not a reason to
emit twice.

### 9.3 The checker — `internal/integration/conformance/check.go`

`Check(steps []Step) error` replays the sequence against the model and returns
one of three failures, mirroring buzz's three:

- `IllegalTransition` — the operation is not legal from `Before`;
- `StateMismatch` — `After` is not what the model computes from `Before` and the
  operation, or an invariant predicate is false;
- `CoverageBreach` — an `ImplBug` step, or a registry operation absent from the
  whole run.

### 9.4 Drive it

Three sources, in increasing order of value:

1. **The fourteen journeys.** Run each with the recording tracer and `Check`
   every trace. This immediately closes the "gutted journey" hole: a no-op
   journey emits no steps and fails coverage.
2. **Generated sequences.** Reuse the property test from
   [adoption 06](06-fuzz-and-property-tests.md#65-property-test-the-transition-relation):
   drive random legal-and-illegal operation sequences against a temp root and
   `Check` the trace. Illegal operations must produce refusal steps, not state
   changes.
3. **Committed replay fixtures.** JSONL traces under
   `internal/integration/conformance/testdata/`, using the naming convention
   from [adoption 07](07-golden-fixture-contract.md): `good.jsonl`,
   `bad_illegal_transition.jsonl`, `bad_state_mismatch.jsonl`,
   `bad_coverage_breach.jsonl`. Reconstruct each from typed Go, assert the
   committed file matches byte-for-byte, then replay. Refresh under
   `SPECD_WRITE_CONFORMANCE_FIXTURES=1`.

### 9.5 `LIMITS.md`

`internal/integration/conformance/LIMITS.md`, in the shape of buzz's, stating at
minimum:

- it is not a proof; coverage is exactly the executions that ran;
- the model is hand-written, so a wrong model and a wrong implementation agree
  and both pass — the model's correctness is a review obligation;
- it observes the decision seam only; a defect in persistence, Git interaction,
  or file scope surfaces here only if the projection reads enough to notice;
- concurrency is out of scope unless a raced sequence surfaces as an illegal
  transition in a captured trace (`internal/integration/concurrency_test.go` is
  the gate that covers contention);
- the exact test commands and the exact test counts;
- the next ratchet.

Cross-link it from `release/gate-limits.md`.

### 9.6 Register the gate

Add a row to `release/release-decision.md`'s gate table and a section to
`release/gate-limits.md`. Add any new exported surface to
`release/surface-inventory.md` — though if this is built as
`internal/integration/conformance` and the production hook stays unexported,
there should be **no** new exported surface at all, which is the target.

## Acceptance

```bash
go test ./internal/integration -run TestConformance -count=1
```

Then prove it bites, and record the results the way `LIMITS.md` records counts:

1. Route a human-only success through the agent class. `IllegalTransition`
   must fire.
2. Construct completion with stale evidence and with an incomplete dependency.
   `StateMismatch` must fire for both invariants.
3. Omit a required operation and feed malformed result output.
   `CoverageBreach` must fire for both observation failures.
4. Refresh the four typed fixtures explicitly and require any changed bytes to
   remain visible for review before replay.

If any mutation class stays green, say which one in `LIMITS.md` rather than
shipping it as complete.

## Do not

- **Do not import production transition logic into the model.** Rule 1. This is
  the failure that makes the whole item worthless, and it is the easy mistake.
- **Do not add TLA+ or Tamarin.** They need a JVM and a separate toolchain for a
  state machine small enough to write in Go, and no CI leg specd runs could
  execute them without new infrastructure. The transferable idea is an
  independent model that judges runtime traces; the JVM is not part of it.
- **Do not let the tracer affect behavior.** Rule 4. If a test passes only with
  tracing on, the tracer is load-bearing and the design is wrong.
- **Do not put trace types in `internal/core`.** They are test support. Core
  exposes a hook; the types live with the checker.
- **Do not let a trace carry file contents, command output, or approver
  identity.** Rule 2.
- **Do not claim the gate proves correctness.** It proves the executions that
  ran matched a hand-written model. `LIMITS.md` says so, in the first paragraph.

## Deferred

- Exhaustive state-space enumeration (a bounded model check over all reachable
  models). Tractable at specd's size and a genuine next ratchet — but only after
  the trace-conformance half is armed and stable. Name it in `LIMITS.md` as the
  next ratchet rather than building it now.
- Tracing the persistence layer. The decision seam is where the interesting
  refusals live; persistence is covered by `journey:10` and the atomicity bite
  tests.

## Acceptance record — 2026-08-06

Applied with the release-journey CLI driver as the observation seam; no
production tracer or exported surface was added. All 15 executable operations
have independent rules and observed steps across all 14 journeys. Five
mutation-class cases retain `IllegalTransition`, `StateMismatch`, and
`CoverageBreach`, including malformed output. Four typed JSONL fixtures are
reconstructed, byte-compared, read, and replayed. A fixed seed exercises 1,000
legal and 1,000 illegal generated model steps; these judge the checker, not the
CLI. Generated sequences through the real CLI are the next ratchet.
