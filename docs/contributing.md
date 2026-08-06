# Contributing

Use this guide to build, test, document, and extend specd without weakening its
enforced guarantees.

Read [`../AGENTS.md`](../AGENTS.md) first — it is the binding workspace guide.
This page is the practical version of it.

## Build and test

```bash
go build -o specd ./cmd/specd
make ci
```

`make ci` runs formatting, `go vet`, the empty-require-set check, release
contract self-checks, benchmark panic-freedom, bounded fuzz bursts, a
`govulncheck` scan, the raced suite, and the `--version`/`--help` smoke. It is
the local projection of `.github/workflows/ci.yml`: a gate added to one is
added to the other in the same commit. CI still calls the commands directly, so
a failure there names the gate and the `windows-latest` leg needs no `make`.

Other targets: `make docs` regenerates the operations document, `make fmt`
formats in place, `make check-offline` is `make check` without the scan that
needs network. On a host without `make`, the raw commands are listed in
[`../AGENTS.md`](../AGENTS.md) §Checks.

Optional git hooks, installed only when you ask for them:

```bash
make hooks   # git config core.hooksPath .githooks
```

`pre-commit` formats staged Go files and re-stages them — it repairs rather
than rejects. `pre-push` runs `go vet` and the suite without `-race`. Both
files open with the list of places they deliberately diverge from CI; a new
divergence gets a line there in the same commit.

Standard library only. `go.mod` having an empty require set is a **release
gate**, not a preference. If you think you need a dependency, you need a smaller
design.

During iteration, run the narrow test first:

```bash
go test ./internal/core -run TestSomething -count=1
```

Then the affected package, then the whole suite before handing off.

When dogfooding this checkout, use `go run ./cmd/specd` or a repository-local
binary. Confirm `command -v specd` before using a global executable; another
project may install a different command with the same name.

## What the release gate checks

`internal/integration/release_test.go` (`TestReleaseQualification`) asserts these
mechanically from repository facts. All of them must stay green:

| gate | how it is checked |
| --- | --- |
| standard-library-only binary | `go.mod` require set must be empty |
| formatting clean | every `.go` file re-formatted in memory and byte-compared |
| generated docs parity | `docs/operations.md` byte-compared with `core.RenderOperationDocs` |
| generated guidance parity | `generate.Render` deterministic, names every agent-visible executable operation |
| all fourteen journeys retained | `requiredJourneys` in `release_test.go` compared with the runner's cases |
| runtime trace conformance | every executable operation and retained journey emits a bounded step checked by an independent test-local model |
| no unowned surface | pending-deletion table in `release/surface-inventory.md` must be empty |
| no dead vocabulary | guidance template, operations doc, and registry help scanned |
| no network or LLM path in the deterministic core | imports parsed for `core`, `plan`, `reconcile`, `generate`, `agentjson`, `context` |

Two more integration tests you will meet:

- `TestSurfaceOwnership` (`subtraction_test.go`) — every exported symbol,
  command, flag, record kind, generated instruction, and adapter hook must map
  to a row in `release/surface-inventory.md`. It fails both ways: live surface
  with no row, and a row describing surface that no longer exists.
- `TestAgentContract` — the agent-facing JSON envelope stays stable.

## The rules that are not negotiable

From [`../AGENTS.md`](../AGENTS.md), restated because most rejected changes
violate one of them:

1. **No bypass.** Never add a flag, env var, or code path that skips evidence,
   approval, authority, scope, or validation. Not "for testing", not behind a
   build tag.
2. **No LLM or network** in gates, graph projection, persistence, or reports.
3. **One owner per contract.** One parser, one resolver, one state transition,
   one evidence rule, one envelope, one report model. If you are about to write
   a second one, you are about to create drift. This includes output: both
   surfaces render the one envelope through `cmd.RenderJSON` and
   `cmd.RenderText`, so a per-operation `RenderX` helper is a second surface
   even when only a test calls it. Project the result in
   `internal/cmd/output.go` instead.
4. **Validate at one boundary.** If `core` already refuses an input, the command
   entry does not restate the check. A duplicated guard drifts, and it usually
   drifts into a weaker refusal than the one it shadows.
5. **Fail closed.** Stale, malformed, future, ambiguous, unsafe, or unauthorized
   input stops. Every refusal carries exactly one legal next action.
6. **Keep the four concepts separate**: activity, readiness, evidence, approval.
7. **Be honest about assurance.** The harness validates scope; only a host
   contains a process. Don't relabel `advisory` as enforced.
8. **No speculative surface.** No interface with one implementation, no config
   for a value that never changes, no field reserved for a deferred domain.
   `TestSurfaceOwnership` enforces this, and it is stricter than review.

## Adding an operation

The registry is the source of truth; everything else projects from it.

1. **Declare it** in `internal/core/operations.go` — id, summary, actor, effect,
   lifecycles, arguments, flags, exits, result type, `AgentVisible`,
   `Executable`, example. An undeclared field fails registry validation rather
   than defaulting to something wider.
2. **Implement the decision** in `internal/core`. Core is the only validation
   boundary; return `failure.Refusal` for invalid input.
3. **Bind one handler** in the `handlers` map in `internal/cmd/dispatch.go`.
   Exactly one per executable id, with no repeated core guard.
4. **Project the result** into the agent envelope in `internal/cmd/output.go`.
   Do not add a per-operation `RenderX` helper. If the result type is not
   projected, the command is registered, documented, and unreachable — this
   actually shipped once, and it is why reachability is tested rather than
   assumed.
5. **Regenerate the docs**:

   ```bash
   SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
   ```

   Never hand-edit `docs/operations.md`; the parity check compares bytes.
6. **Add a surface-inventory row** in `release/surface-inventory.md` mapping it
   to a journey or an invariant.
7. **Add a bite test** for every refusal, asserting its stable code and one
   legal next action.
8. **Cover it with a journey** if it is user-reachable, in
   `internal/integration/release_journeys_test.go`.
9. **Update hand-written docs and run `make ci`.** Change only the guide that
   owns the workflow or guarantee; do not create a second command table.

If the operation is human-only, `AgentVisible` is false and there is no
agent-callable form. Do not add a flag that reveals one.

## Adding a gate

1. Implement a planning gate in `internal/core/gates/planning.go`, a production
   gate in `production.go`, or a repository-fact gate in
   `internal/integration/release_test.go`. Register planning and production
   gates in `registry.go`.
2. Add its exact row to `release/release-decision.md`.
3. Add the same heading to `release/gate-limits.md` and state what green does
   not establish. `gateLimitsComplete` enforces the pair.
4. Prove it bites: construct or mutate the forbidden state, confirm the gate
   fails, revert the mutation, and retain the smallest automated bite case.
5. Run `make ci`.

A planning or production gate returns findings with a code, a `file:line`
location, a message, and a `fix:`. The fix is required because a finding a
caller cannot act on is noise.

Changing the registry changes its version, which makes existing approvals stale
(`registry_version_changed`). That is correct behavior, and it means gate
changes are not free — they cost every in-flight change a re-approval.

## Adding a refusal

1. Create the refusal at the deciding trust boundary with
   `internal/core/failure.New(code, root, path, reason, next)`. The code is
   stable machine-readable surface; use an existing code when the condition and
   recovery are identical.
2. Give it exactly one legal next action. `failure.New` panics on an empty code,
   reason, or action, so a dead-end refusal cannot hide in a test.
3. Keep presentation in `internal/cmd/output.go`. Core owns the decision and
   structured facts; it does not format terminal or JSON output.
4. Add one bite test that constructs the forbidden state and asserts the code
   and next action. Reuse `assertActionableRefusal` in `internal/core` where it
   fits.
5. If the refusal is user- or agent-visible, extend the owning journey and the
   troubleshooting table, then run `make ci`.

## Contributor gotchas

1. **Operation docs look like a renderer failure after a registry edit.** The
   cause is usually skipped regeneration. Run `make docs` and commit
   `docs/operations.md` with the registry change.
2. **A generated agent guide reports drift after a hand edit.** Bytes between
   managed markers are harness-owned. Restore them through the refresh path;
   put local notes outside the markers.
3. **A build fails in `subtraction_test.go` after an unrelated Go edit.** A new
   exported symbol lacks an owner. Add the exact inventory row or make the
   symbol private.
4. **The dead-vocabulary gate points at generated output.** The source registry
   introduced a noun outside `AGENTS.md`'s vocabulary. Rename it at the owner,
   then regenerate; do not patch the projection.
5. **A hand edit under `.specd/` causes a refusal.** Restore the managed bytes
   from a trusted commit and rerun the operation named by the refusal. Editing
   state, history, evidence, or task markers is never a repair.
6. **A Windows checkout changes every golden fixture.** `.gitattributes` pins
   line endings. Re-check out through Git with those attributes; do not refresh
   every golden from CRLF bytes.

## Working on the docs

The `docs/` set is nine hand-written pages plus one generated. Rules:

- **One source of truth per fact.** `docs/operations.md` is generated and is the
  only place flags, exit codes, and lifecycles are enumerated. Guides link to
  it; they never restate a flag table.
- **Give every page one audience and subject.** Use direct language, project
  vocabulary, scannable headings, and a useful next link.
- **Don't document surface that doesn't exist.** If it is not in
  `release/surface-inventory.md`, it gets no page.
- **Run what you write.** Command output in the guides is captured from real
  runs, not composed. If you change behavior, re-run the affected walkthrough.
- **Cite the enforcement.** A claim about a guarantee should name the code or
  the journey that proves it, so the next person can check rather than trust.
- **Working notes go in the change that owns them**, under `.specd/`, not in
  `docs/` and not at the repository root.

## Repository layout

| path | what it is |
| --- | --- |
| `cmd/specd/`, `internal/cli/`, `internal/cmd/` | entry, argv parsing, dispatch |
| `internal/core/` | lifecycle, approval, readiness, scope, evidence, completion |
| `internal/core/gates/` | deterministic validation |
| `internal/core/{state,record,persist,lock,path}/` | durability primitives |
| `internal/plan/`, `internal/reconcile/` | authored artifacts in, accepted specs out |
| `internal/context/` | bounded task context |
| `internal/exec/`, `internal/core/verify/` | bounded process execution and evidence |
| `internal/agentjson/`, `internal/generate/`, `internal/host/` | the agent-facing surface |
| `internal/integration/` | journeys, release gates, surface ownership |
| `.github/workflows/` | platform matrix, race tests, vet, and release publication |
| `.specd/` | specd's own managed root; specd plans its changes here, like any other project |
| `release/` | release decision and surface inventory |

## Cutting a release

Write the release notes under `## [Unreleased]`, then prepare the reviewable
metadata from a clean `main` checkout:

```bash
make release-prep VERSION=x.y.z
gh pr create --base main --head release/x.y.z --title "Release vx.y.z"
```

The preparation script promotes the human-written notes, creates the release
branch and commit, and creates no tag. Merging that same-repository PR is the
human gate; `.github/workflows/auto-tag.yml` checks the prospective contract and
creates one annotated `vx.y.z` tag on the merge commit.

The default GitHub token used by auto-tag does not trigger the publishing
workflow. Start `release.yml` manually from the existing tag and supply the
matching version. The workflow refuses a branch ref or mismatched version, so
manual dispatch is retry-only and can never publish from `main`.

A published tag is immutable. Never move or delete it; supersede a bad release
with a new version.

## Before you hand off

- the narrow test for what you changed, then `make ci`;
- generated files regenerated, not edited;
- `release/surface-inventory.md` updated if you added or removed surface;
- assumptions, deliberate simplifications, and any mismatch between docs, source,
  and tests reported rather than silently resolved.

A deliberate shortcut with a known ceiling gets a `ponytail:` comment naming the
ceiling and the upgrade trigger. There are a few in the tree already; follow the
shape.

Report any mismatch among docs, source, tests, security policy, and release
evidence. Do not silently choose the more convenient claim.
