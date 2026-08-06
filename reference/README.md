# reference/ — buzz-derived production hardening for specd

This directory is a working brief, not shipped documentation. It exists so a
coding agent can take specd from "young 0.x with unusually good release
discipline" to the operating standard demonstrated by the `buzz-reference/`
checkout (`block/buzz`), without importing buzz's stack, scale, or scope.

Nothing here is user-facing. Nothing here belongs in `docs/`. When an adoption
item lands, the durable record of it goes in the repository's own files —
`AGENTS.md`, `release/release-decision.md`, `release/surface-inventory.md`,
`CHANGELOG.md` — and the adoption doc is marked applied.

## Read in this order

| # | Doc | What it answers |
| --- | --- | --- |
| 1 | [buzz-analysis.md](buzz-analysis.md) | What buzz actually is and which of its practices are load-bearing |
| 2 | [gap-analysis.md](gap-analysis.md) | Where specd already exceeds buzz, and where it is behind |
| 3 | [patterns.md](patterns.md) | The transferable patterns, named and numbered (P1–P14) |
| 4 | [apply-plan.md](apply-plan.md) | Phasing, sequencing, and the definition of done |
| 5 | [adoption/](adoption/) | One doc per work item, each independently applicable |

## Hard constraints on every adoption item

These come from [`AGENTS.md`](../AGENTS.md) and are not negotiable by anything
in this directory. An adoption item that violates one is wrong, and this
directory is the thing that is wrong, not `AGENTS.md`.

1. **Standard library only.** `go.mod` having an empty require set is a release
   gate. No adoption item adds a runtime dependency. Test-only dependencies are
   also refused — Go's own `testing`, `testing/quick`, and native fuzzing cover
   every case here.
2. **One output owner.** Both surfaces project one envelope through
   `cmd.RenderJSON` / `cmd.RenderText`. No adoption item adds a second render
   path, not even for a diagnostic.
3. **One validation boundary.** If `core` refuses an input, the command entry
   does not restate it.
4. **No new unowned surface.** Every new exported symbol needs a row in
   `release/surface-inventory.md` with a `journey:`, `invariant:`, or
   `contract:` owner, or `internal/integration/subtraction_test.go` fails the
   build. Prefer unexported and test-local surface; most items here need zero
   new exported symbols.
5. **No bypass.** Nothing here weakens evidence, approval, authority, scope, or
   validation. Several items *strengthen* them by proving the existing gates
   bite.
6. **No dead vocabulary.** Do not introduce nouns beyond the `AGENTS.md`
   vocabulary table on user- or agent-visible surface. Internal test vocabulary
   (`trace`, `model`, `bite`) stays internal and out of help text, generated
   guidance, and `docs/`.
7. **Honesty over coverage.** specd's core cultural asset is that it claims only
   what a green run observed. Every gate added here ships with what it does
   *not* catch. A gate whose limits are undocumented is worse than no gate,
   because it invites a claim nothing supports.

## How to apply an item

Each `adoption/NN-*.md` is self-contained and states its own acceptance check.
Per `AGENTS.md` working rule 2, non-trivial items should be planned as a change
under `.specd/` rather than an ad-hoc edit — several of these items are exactly
the kind of work specd exists to run through its own loop.

Apply order is in [apply-plan.md](apply-plan.md). Items within a phase are
independent; items across phases are not.

After each item:

```bash
go test ./... -race -count=1 && go vet ./...
```

If the item changed the operation registry:

```bash
SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
```
