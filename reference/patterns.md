# Transferable patterns (P1–P14)

Each pattern is stated in its general form, grounded in the buzz artifact that
demonstrates it, and translated into specd's constraints. The adoption doc that
applies it is named at the end of each entry.

---

## P1 — One command reproduces CI

**Form.** A contributor should never have to assemble the gate from memory. One
command runs exactly what CI runs, and the project states that equivalence.

**buzz.** `just ci` = `check` + `test-unit` + desktop build + tauri check/test +
web build + mobile test. `CONTRIBUTING.md`: "This is the same check that runs in
CI. PRs that fail `just ci` will not be merged."

**specd.** The gate is already small — `go test ./... -race -count=1`,
`go vet ./...`, `gofmt -l .`, the empty-require-set check, and the operations
regeneration. It is *scattered* across `AGENTS.md`, `README.md`, and
`docs/contributing.md` rather than executable. A `Makefile` with `make ci`
collects it without adding a dependency, and CI keeps calling the underlying
commands directly so the workflow stays readable and Windows legs stay
make-free.

→ [adoption/01](adoption/01-local-gate-runner.md)

---

## P2 — Cheap and auto-fixing at commit, fast and verifying at push

**Form.** Hooks are tiered by cost. Commit-time hooks *fix* rather than reject.
Push-time hooks *verify* and do not repeat commit-time work. Expensive work is
CI-only. Hooks are opt-in to install and never mandatory to bypass.

**buzz.** `lefthook.yml`: `pre-commit` runs formatters with `stage_fixed: true`;
`pre-push` runs unit suites with no overlap; "Builds are CI-only."

**specd.** `gofmt -w` at commit, `go vet` + the fast test subset at push. No
lefthook — a checked-in `.githooks/` directory plus `git config core.hooksPath`
does the same with zero dependencies and works on every platform specd claims.

→ [adoption/02](adoption/02-git-hooks.md)

---

## P3 — A gate must be provable, and its blind spots must be written down

**Form.** Every gate ships with (a) a test that fails when the gate's logic is
mutated, and (b) a document saying what a green run does *not* establish.

**buzz.** `LIMITS.md`: "The runtime conformance harness is **not a proof.**"
Then eight named things it does not catch, then the exact test command and the
exact test count. And: "Coverage breach is load-bearing. Without it, trace
conformance is decorative logging."

**specd.** This is the closest cultural match in the whole comparison —
`release/release-decision.md` already distinguishes proven from unproven and
retracted a release for overstating. The gap is per-gate: the release gate table
lists nine gates and zero blind spots.

→ [adoption/05](adoption/05-mutation-bite-tests.md),
[adoption/08](adoption/08-limits-docs.md)

---

## P4 — The checker must be independent of the thing it checks

**Form.** A component that judges correctness must not share code with the
component it judges. Shared helpers let one bug satisfy both sides.

**buzz.** `buzz-conformance` re-implements the spec's transition relation in
Rust rather than calling any production reducer, and refuses to reuse
`buzz_core::CommunityId` — "sharing normalization helpers between the emitter
and the checker would let a bug in the helpers hide itself from both — exactly
the failure the skill calls out."

**specd.** specd's lifecycle and task-activity transitions live in
`internal/core/tasktransition.go`, `approval_status.go`, `readiness.go`. A
conformance model must restate the legal transitions in test-local code, not
import the production transition function. This is the single most important
rule in adoption 09; getting it wrong produces a tautology.

→ [adoption/09](adoption/09-model-conformance.md)

---

## P5 — Coverage breach fails closed

**Form.** When an observability harness is armed on a code path, *entering that
path without emitting an observation* is itself a failure. Otherwise a new code
path silently escapes the gate and the gate stays green.

**buzz.** `EmitGuard::arm` at seam entry; `Drop` records `ImplBug` if no emit
reached the tracer. `LIMITS.md`: "Without it, trace conformance is decorative
logging" — and, honestly, "If a new endpoint is added that bypasses
`EmitGuard::arm`, the gate is blind… that's enforced by code review, not by the
harness."

**specd.** Applies directly: a new operation added to the registry that no
journey exercises, or a new state transition no model case covers, must fail
rather than pass silently. specd already has the shape of this in
`subtraction_test.go` (unowned surface fails). Extend the same reflex to
transitions and refusal codes.

→ [adoption/09](adoption/09-model-conformance.md)

---

## P6 — Golden fixtures are a byte contract with an explicit refresh switch

**Form.** Committed fixtures are asserted byte-for-byte so a schema change
cannot pass without updating them, and refreshing them requires an explicit
environment variable so it can never happen by accident.

**buzz.** Fixtures reconstructed from typed Rust, compared byte-for-byte "so a
schema-change PR must update the fixtures", refreshed only with
`BUZZ_CONFORMANCE_UPDATE=1`.

**specd.** The mechanism already exists — `SPECD_WRITE_OPERATION_DOCS` and
`SPECD_WRITE_AGENT_JSON`. It is not yet applied to `internal/integration/testdata/release/`
or to the plan/reconcile fixtures.

→ [adoption/07](adoption/07-golden-fixture-contract.md)

---

## P7 — Adversarial fixtures named for what they violate

**Form.** For every invariant, commit an input that violates it, named after the
violation, and assert the exact refusal.

**buzz.** `good.jsonl`, `bad_host_channel_mismatch.jsonl`,
`bad_coverage_breach.jsonl`, `bad_foreign_row_leak.jsonl`. The filename is the
test's documentation.

**specd.** The eight foundation invariants in `surface-inventory.md`
(validation, approval, authority, scope, evidence, staleness, atomicity,
fail-closed) are the natural fixture axis. Journeys 04–11 already cover refusal
and recovery; the gap is that a reader cannot enumerate the violations from the
fixture tree.

→ [adoption/07](adoption/07-golden-fixture-contract.md)

---

## P8 — Fuzz what parses, property-test what transitions

**Form.** Anything consuming externally shaped bytes gets a fuzz target with a
seed corpus. Anything with a transition relation gets generated-sequence
testing.

**buzz.** `proptest_checker.rs` over the transition relation.

**specd.** Go's native `testing.F` needs no dependency. Targets:
`plan.ParseTasks`, `plan.ParseSections`, `plan.ParseDeltas`, `record` decoding,
`state` decoding, and change/capability name resolution in `core/path`. The
invariant a fuzz target asserts is not "no error" — it is **no panic, and every
refusal carries exactly one legal next action**, which is specd's stated
contract and therefore the right fuzz oracle.

→ [adoption/06](adoption/06-fuzz-and-property-tests.md)

---

## P9 — A capacity claim needs an executable model

**Form.** Do not claim or disclaim scale in prose. Ship a benchmark or a model
that produces the number, and test the model.

**buzz.** `perf/RELAY_BUS_SCALING.md` alongside `relay_bus_scaling.py` and
`test_relay_bus_scaling.py`.

**specd.** The README currently says "scale and long-lived changes are not yet
proven" — correct today, but the honest resolution is measurement, not a
permanent disclaimer. `go test -bench` over history append/replay, state
read-modify-write, readiness projection, and context assembly turns the sentence
into a number with a date, matching how `release-decision.md` treats every other
claim.

→ [adoption/10](adoption/10-benchmarks-and-scale.md)

---

## P10 — Release is a PR, tagging is automated, dispatch is retry-only

**Form.** The human gate on a release is merging a metadata PR. Tag creation is
automated from the merge and uses a credential distinct from the default CI
token. Manual dispatch exists only to retry an existing immutable tag, never to
build from a branch.

**buzz.** `just release-<lane>` opens `version-bump/<v>`; merging triggers
`auto-tag-on-release-pr-merge.yml`, which tags with an App token while "the
workflow's default `GITHUB_TOKEN` remains read-only and is never used to create
a tag." Manual dispatch "is only a retry mechanism for an existing immutable
`v<version>` tag. It cannot build from `main`."

**specd.** `release/tag-contract.sh` already refuses a lightweight tag or a
version with no changelog section — the contract exists, the automation does
not. Note the philosophical fit: specd is a harness that makes humans approve
transitions and machines enforce them. Its own release process should be the
same shape.

→ [adoption/11](adoption/11-release-automation.md)

---

## P11 — Documentation is organized by failure mode

**Form.** The largest sections of an agent- or contributor-facing document are
about what goes wrong, written as symptom → cause → fix, including the sentence
explaining why the trap is expensive.

**buzz.** `AGENTS.md` §Common Gotchas; `TESTING.md` and `RELEASING.md`
troubleshooting tables; "That looks exactly like a product bug rather than a
build mistake, so it burns real time."

**specd.** `docs/troubleshooting.md` exists and is the right shape. The gap is
contributor-side: nothing tells a contributor what will bite them when changing
the operation registry, adding a gate, or regenerating docs.

→ [adoption/12](adoption/12-architecture-doc.md),
[adoption/14](adoption/14-contributor-contract.md)

---

## P12 — Every rule names its enforcement, and forbids the escape hatch

**Form.** A rule states the mechanism that enforces it and pre-refuses the
obvious workaround.

**buzz.** "Hard ceiling: **1000 lines/file**, enforced by
`mobile/scripts/check-file-sizes.mjs` via `just mobile-check`… **If the guard
trips, split the file — never bump the limit or add an override to slip under
it.**"

**specd.** `AGENTS.md` already does this well ("The dead-vocabulary gate in
`internal/integration/release_test.go` enforces this"). Apply the same phrasing
to every rule added by these adoptions, and add the pre-refused escape hatch —
specd's version is "never add a bypass", already stated, which should be
restated locally wherever a new gate lands.

→ every adoption doc's "Do not" section

---

## P13 — Machine-readable registry for anything currently claimed in prose

**Form.** If a property is asserted in a sentence and consumed by more than one
reader, promote it to data and generate the sentence.

**buzz.** `preview-features.json`; `kind.rs` as the authoritative kind registry;
`deny.toml` ignores carrying reason and removal condition as structured data.

**specd.** Already the project's central mechanism — the operation registry
generates `docs/operations.md`, `--help`, and the managed block of `AGENTS.md`.
The remaining prose claim is maturity: "the production profile is experimental"
appears in `README.md`, `docs/README.md`, and `SECURITY.md` with no single
source. Promote it.

→ [adoption/13](adoption/13-experimental-registry.md)

---

## P14 — Name the trade-off and the next ratchet

**Form.** When a simplification gives something up, the document says what was
given up and what would reverse it. When a gate is partially armed, the document
says which half and what will arm it.

**buzz.** "The simplification trades away a separate stabilization line…
Add a dedicated hotfix flow later if a release actually needs isolation from
`main`." And: "the read-seam half of the gate is **not yet armed**… The
integration replay is the **next** ratchet."

**specd.** Already native — `release-decision.md` names deferred domains and
their triggers, and `AGENTS.md` says complexity "must earn its place through
real use." Every adoption doc here therefore ends with what it defers and what
would trigger it, so this work does not become the thing that quietly grew the
project.

→ every adoption doc's "Deferred" section
