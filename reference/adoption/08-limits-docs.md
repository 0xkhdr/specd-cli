# 08 — Document what each gate does not catch

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P3](../patterns.md#p3--a-gate-must-be-provable-and-its-blind-spots-must-be-written-down), [P14](../patterns.md#p14--name-the-trade-off-and-the-next-ratchet) | 2 | small | low | applied 2026-08-06 |

## Why

`crates/buzz-conformance/LIMITS.md` is the single most copyable document in
buzz. Its opening:

> The runtime conformance harness is **not a proof.** It says only this: *for
> the executions that actually ran with tracing on*, the relay's ingest/read
> decisions matched a trace the spec accepts. Coverage is exactly the set of
> code paths exercised — no more, no less.
>
> This file says what the gate **doesn't** catch, so reviewers and operators
> don't read more into a green run than is there.

It then names eight blind spots, states the exact test commands and counts, says
which half of the gate is not yet armed, and names the next ratchet.

specd's `release/release-decision.md` is already in this family — it separates
proven from unproven, records which gates stopped running and why, and states
that a claim is earned by a run rather than a runner. The gap is **granularity**:
the release-gate table lists nine gates and zero blind spots. A reader learns
that "no unowned surface" is green; nothing tells them that it proves ownership
of *exported* symbols and says nothing about unexported behavior, or that a row
can be satisfied by an owner string nobody re-derived.

This item costs one document and materially raises the honesty of every claim
specd publishes.

## Change set

Create `release/gate-limits.md`. Link it from `release/release-decision.md`'s
gate table (one line, immediately after the table) and from
`docs/README.md` §Evaluate specd.

Structure, per gate, kept short — three to six lines each:

```markdown
# Gate limits

`release/release-decision.md` records whether each gate is green.
This file records what a green gate does **not** establish, so a reader does
not infer more from a passing run than the gate observes. It is the companion
to that document, not a substitute: if the two disagree, the gate's
implementation wins and both documents are bugs.

## standard-library-only default binary

Establishes: `go.mod`'s require set is empty at test time.
Does not establish: that no code path shells out to an external program, that
build tags do not introduce a dependency under another configuration, or that
the standard library specd links is free of advisories. That last one is
[govulncheck](../reference/adoption/04-supply-chain.md)'s job, and it is a
separate gate with separate limits.

## generated docs parity

Establishes: `docs/operations.md` is byte-identical to the registry projection.
Does not establish: that the registry describes what the commands do. A wrong
usage string is rendered faithfully and passes.

## no unowned surface

Establishes: every exported symbol has a row, and every row describes surface
that exists.
Does not establish: that the owner is *correct*. `journey:01` in a row is a
string; nothing re-derives that journey 01 actually exercises the symbol.
Ownership is a review obligation the gate makes visible, not one it verifies.

## no dead vocabulary

Establishes: the guidance template, generated operations document, and registry
help contain no forbidden noun.
Does not establish: anything about `docs/`, `README.md`, source comments, or
refusal message bodies assembled at runtime.

## all fourteen required journeys retained

Establishes: the runner still contains fourteen journeys with the required
names.
Does not establish: that a journey still asserts what its name says. A journey
gutted to a no-op keeps its name and passes.

## no network or LLM path in the deterministic core

Establishes: the named packages' import graphs contain no network or LLM
import.
Does not establish: that a package outside the named list is clean, or that a
future package is added to the list. The list is maintained by hand.

## suite and vet green on four platforms

Establishes: the suite passed on the four runners on the recorded date.
Does not establish: that the loop has been driven by hand on anything but
linux/amd64 — `README.md` and `SECURITY.md` already state this, and the tiers
exist so neither claim is inferred from the other.

## Cross-cutting limits

- Every gate observes a checkout, not a deployment. Nothing here says anything
  about the host specd runs on, which is why host assurance is `advisory`
  unless the host proves containment.
- A gate proves the invariant held for the inputs it saw. Coverage of the
  refusal paths is the subject of the bite tests
  (reference/adoption/05) and the fuzz corpora (reference/adoption/06).
```

Fill each section against the real gate list in
`release/release-decision.md` — the nine rows there are the exact table of
contents, so the two documents stay in sync structurally.

## The next-ratchet section

End the file the way `LIMITS.md` does — with what would close the largest
remaining blind spot, named, so the document records intent rather than
implying completeness:

```markdown
## Next ratchet

The largest current blind spot is that "all fourteen journeys retained" checks
names, not behavior. Model conformance
(reference/adoption/09-model-conformance.md) closes it by asserting that the
transitions a journey drives are exactly the transitions an independent model
permits — a gutted journey then fails on coverage, not on its name.
```

## Acceptance

- Every row in `release-decision.md`'s two gate tables has a section here.
- Adding a gate without adding its limits section is caught: extend
  `internal/integration/release_test.go` so the gate names parsed from
  `release-decision.md` must each appear as a heading in `gate-limits.md`. That
  is the same parse-the-document technique `release_test.go` already uses, so it
  adds no new mechanism.
- The relative-link gate already covers the new cross-links; confirm it passes.

## Do not

- Do not soften a limit because it is uncomfortable. "Ownership is a review
  obligation the gate makes visible, not one it verifies" is the sentence that
  makes the gate trustworthy.
- Do not merge this into `release-decision.md`. That document has a parsed
  structure and exactly one dated decision; adding prose sections to it risks
  the parser and buries the limits.
- Do not write limits for a gate that does not exist yet. Each adoption item
  adds its own section when it lands.

## Deferred

Per-invariant limits (what "scope is enforced" does not cover). That belongs
with [adoption 09](09-model-conformance.md), where the invariant list becomes
machine-readable and the limits can hang off it.

## Acceptance note — 2026-08-06

Every release gate row has an exact section in `release/gate-limits.md`.
`gate limits complete` parses both gate tables and fails when a heading is
missing. User documentation links the limits beside the release decision.
