# Apply plan

Sequencing, dependencies, and the definition of done for the adoption set.

## Phases

Items within a phase are independent and may be applied in any order or in
parallel. Items in a later phase assume the earlier phases landed.

### Phase 1 — Make the gate reproducible (small, low risk) — applied 2026-08-06

| Item | Depends on | Deliverable |
| --- | --- | --- |
| [01 local gate runner](adoption/01-local-gate-runner.md) | — | `Makefile`, `make ci` |
| [02 git hooks](adoption/02-git-hooks.md) | 01 (`make hooks`) | `.githooks/pre-commit`, `.githooks/pre-push` |
| [03 CI hardening](adoption/03-ci-hardening.md) | — | timeouts; tag contract on every PR |
| [04 supply chain](adoption/04-supply-chain.md) | 01 (`make vuln`) | `govulncheck` job, `CODEOWNERS`, finding policy |

Phase 1 changes no production code. It is safe to apply as ordinary commits
rather than through the `.specd/` loop.

### Phase 2 — Make the gates provable (medium) — applied 2026-08-06

| Item | Depends on | Deliverable |
| --- | --- | --- |
| [05 mutation-bite tests](adoption/05-mutation-bite-tests.md) | — | bite audit; refusal-code assertions; the `AGENTS.md` rule |
| [06 fuzz and property tests](adoption/06-fuzz-and-property-tests.md) | — | five fuzz targets, seed corpora, the transition table |
| [07 golden fixture contract](adoption/07-golden-fixture-contract.md) | — | byte contract, `bad_<invariant>_*` naming, fixture README |
| [08 limits docs](adoption/08-limits-docs.md) | 05, 06, 07 land first so their limits are writable | `release/gate-limits.md` + its gate |

Apply 05 before 06: fuzz targets assert refusal shape, and 05 is where that
assertion style is established. Apply 08 last in the phase — it documents what
the others do not catch.

### Phase 3 — Close the claims specd's own documents leave open (large) — applied 2026-08-06

| Item | Depends on | Deliverable |
| --- | --- | --- |
| [09 model conformance](adoption/09-model-conformance.md) | 05, 06 (transition table), 07 (fixture conventions) | independent model, tracer, checker, `LIMITS.md` |
| [10 benchmarks and scale](adoption/10-benchmarks-and-scale.md) | — | four benchmarks, `release/scale.md`, narrowed disclaimer |
| [11 release automation](adoption/11-release-automation.md) | 03 (contract runs on every PR) | `release/prepare.sh`, `auto-tag.yml`, immutability rule |

09 is the big one. Plan it as a change under `.specd/` — it is exactly the kind
of work specd exists to run through its own loop, and driving it that way
produces a second hand traversal, which is itself evidence the release record
currently lacks.

### Phase 4 — Documentation completeness (small–medium) — applied 2026-08-06

| Item | Depends on | Deliverable |
| --- | --- | --- |
| [12 architecture doc](adoption/12-architecture-doc.md) | benefits from 09's layer clarity | `ARCHITECTURE.md` |
| [13 experimental registry](adoption/13-experimental-registry.md) | 10 (scale numbers to reference) | `internal/core/maturity.go` + projection + gate |
| [14 contributor contract](adoption/14-contributor-contract.md) | 01–08 (the cookbooks reference them) | commit/merge policy, cookbooks, gotchas |

## Cross-cutting obligations

Every item, without exception:

1. **`go test ./... -race -count=1 && go vet ./...` green**, on the item's own
   commit — not deferred to the end of the phase.
2. **No new runtime dependency.** The empty-require-set gate is the check;
   run it.
3. **New exported surface has an owner row** in
   `release/surface-inventory.md`, or the build fails. Most items here should
   add zero exported symbols; if an item adds several, that is a signal to look
   for a smaller shape first.
4. **Regeneration is committed with the change** that caused it —
   `docs/operations.md` and the managed `AGENTS.md` block.
5. **A new gate ships with its limits section** in `release/gate-limits.md`
   (once [08](adoption/08-limits-docs.md) exists) and its row in
   `release/release-decision.md`.
6. **A new gate ships with proof it bites.** Mutate what it protects, confirm
   failure, revert, record what you tried and what happened.
7. **`CHANGELOG.md` gets an entry** for anything a user or an agent can observe.
   Internal test infrastructure does not.

## Definition of done for the whole set

The set is complete when all of these are true:

- `make ci` is green and is the documented local gate in `AGENTS.md`,
  `README.md`, and `docs/contributing.md`.
- Every one of the eight foundation invariants has a named bite test asserting a
  refusal code and one legal next action.
- Every parser listed in [06](adoption/06-fuzz-and-property-tests.md) has a fuzz
  target with a committed seed corpus, and any crash found is committed as a
  regression corpus entry.
- `release/gate-limits.md` has a section for every gate in
  `release/release-decision.md`, and adding a gate without its section fails the
  build.
- The conformance gate fails on all four mutations listed in
  [09](adoption/09-model-conformance.md)'s acceptance section, and
  `internal/integration/conformance/LIMITS.md` states which half is armed.
- `release/scale.md` carries measured numbers, a named machine, and a date, and
  `README.md`'s scale sentence points at it rather than standing alone.
- A release is cut by opening a `release/<version>` PR and merging it; no human
  types a tag.
- `ARCHITECTURE.md` exists and every rule in it names its enforcing test.
- No maturity claim appears in more than one authored place.
- `go.mod`'s require set is still empty.

## What must not have changed

Check these explicitly at the end of each phase. They are the properties that
make specd worth hardening in the first place:

- zero runtime dependencies;
- one output owner; one validation boundary; one canonical parser per contract;
- no bypass for evidence, approval, authority, scope, or validation;
- no network or LLM path in the deterministic core;
- every published claim still earned by a dated observation;
- the vocabulary table unchanged on user- and agent-visible surface.

If an adoption item can only be applied by weakening one of these, the item is
wrong. Stop, and record why in the item's doc rather than proceeding.

## Suggested first commit

[01](adoption/01-local-gate-runner.md) plus
[03](adoption/03-ci-hardening.md)'s tag-contract step. Together they take the
gate from "assembled from three documents" to "one command, and the release
contract is checked on every change" — the smallest change that makes every
later item cheaper to verify.
