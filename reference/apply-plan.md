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

### Phase 5 — close the set (small) — applied 2026-08-06

A review of the applied set found two defects and one piece of stale state. The
work is recorded here rather than as a new adoption item, because none of it is
a new pattern: each is an item that was marked applied without its acceptance
being run.

| Item | What was wrong | Fix |
| --- | --- | --- |
| [13](adoption/13-experimental-registry.md) | upgrading a gated platform to `proven` in the registry alone left the suite green — the registry was decoration for every row no summary sentence named | platform levels are checked against the supported tier parsed from `release-decision.md`, and every platform row must carry the raced-suite observation date; both bites in `TestMaturityGateBites` |
| 10, 12, 13, 14 | marked applied with no acceptance note, unlike 01–09 | acceptance run and recorded; [11](adoption/11-release-automation.md) records what a local rehearsal can and cannot observe |
| — | `status` and `next` leaked a raw filesystem error for a change with no `state.json`, with `run specd status <change>` as the recovery — the command that had just failed | the refusal moved into `readCheckState`, the one reader of state, so every loader refuses `check_state` with one legal next action; bite in `TestUnreadableStateRefusesOnEveryLoaderBite` |

Still open, and deliberately not closed by writing a date:

- The raced-suite and `go vet` rows in `release/release-decision.md`, and every
  `Observed` value in `internal/core/maturity.go`, are dated 2026-08-01 — before
  phases 1–4 added conformance, fuzz, benchmarks, and the registry. CI has run
  the larger suite on all four legs since. Re-dating requires reading a green
  four-platform run and moving the decision row and the registry together; the
  new date gate now forces them to move together.
- No release has been cut through the release-PR path, so the definition of done
  below is not yet met on that line.

Driving this phase through the `.specd/` loop produced one finding of its own,
recorded as limitation 9 in `release/release-decision.md`: a task's verification
was authored with a Markdown-escaped `\|` in a `go test -run` pattern, `verify`
correctly refused the vacuous run, and no operation could then repair the
approved plan. That is the second-hand traversal working as
[09](adoption/09-model-conformance.md) argues it should — the loop found a
defect in the loop.

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
- Every protected foundation invariant has a discovered `bad_*` release
  fixture that drives its refusal route and asserts the exact refusal code and
  recovery instruction; no named fixture is unread.
- `release/gate-limits.md` has a section for every gate in
  `release/release-decision.md`, and adding a gate without its section fails the
  build.
- The conformance gate independently models and observes every executable
  operation, retains illegal-transition, state-mismatch, coverage-breach, and
  malformed-output bites, replays typed byte fixtures, and states its exact
  observation seam in `internal/integration/conformance/LIMITS.md`.
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
