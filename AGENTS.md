# specd contributor and agent guide

## Mission

specd is a spec-driven coding harness: process enforcement lives in a
deterministic, local, tool-gated pipeline instead of in an agent's context
window. The agent reasons; the harness enforces.

Go, standard library only, zero runtime dependencies, one static binary.

## Read first

1. [`docs/`](docs/README.md) — what specd does today, from a user's and an
   agent's side. [`docs/contributing.md`](docs/contributing.md) is the
   practical build, test, and extension guide; this file is the binding one.
   Documentation describes shipped behavior, so when it disagrees with source,
   the source wins and the doc is a bug.
2. [`release/release-decision.md`](release/release-decision.md) — exactly what
   has been proven and what has not.
3. [`release/surface-inventory.md`](release/surface-inventory.md) — the one
   ownership mapping. Every exported symbol maps to one exercised journey, one
   protected invariant, or one named external contract.
4. [`ARCHITECTURE.md`](ARCHITECTURE.md) — code layers, contract owners,
   durability, trust boundaries, and the tests that enforce each rule.
5. Source and tests for the domain being changed.

## Vocabulary

Use these nouns consistently:

| Term | Meaning |
| --- | --- |
| root | One project-local or explicitly selected planning home |
| spec | Accepted current behavioral truth |
| change | Proposed work being planned and implemented |
| proposal | Why the change exists and its scope/non-goals |
| requirements | Observable behavior that must be true |
| design | Boundaries, interfaces, invariants, failure, integration, alternatives, owner |
| tasks | Atomic executable work with dependencies, declared files, verification, acceptance |
| verify | A current observation/evidence record, never completion by itself |
| complete | A harness-owned transition that consumes applicable passing evidence |
| context | Bounded read input supplied to one task |
| authority | Machine-checked permission for a role/task/scope, not prose permission |
| approval | Human semantic authorization; an agent must not self-approve |

Do not introduce nouns such as initiatives, collections, or workspaces unless a
concrete requirement proves one is needed. The dead-vocabulary gate in
`internal/integration/release_test.go` enforces this on user- and agent-visible
surface.

## The loop

```text
init → new → author proposal/delta/design/tasks → check → human approve
→ next → context → start → edit declared files → verify → complete
→ human sync → sync → archive
```

State, evidence, and task markers are harness-owned. Never hand-edit
`.specd/` state, history, evidence ledgers, or task markers.

## Design rules

- Keep Markdown and Git as the user-visible source of truth.
- Prefer one canonical parser/model per contract; do not duplicate resolution,
  task parsing, command metadata, or JSON envelopes. The single-owner roles are
  gated in `internal/integration/subtraction_test.go`.
- One output owner. Both surfaces project the one envelope through
  `cmd.RenderJSON` and `cmd.RenderText`; a per-operation `RenderX` helper is a
  second surface even when only a test calls it. Project the result in
  `internal/cmd/output.go` instead.
- One validation boundary. If `core` already refuses an input, the command entry
  does not restate the check. A duplicated guard drifts, and it usually drifts
  into a weaker refusal than the one it shadows.
- Keep deterministic gates, graph projection, persistence, and reports free of
  LLM and network calls.
- Keep activity, readiness, evidence, and human approval as separate concepts.
- Preserve the guarantees: atomic state writes, revision checks, declared file
  scope, bounded context, fail-closed stale/ambiguous state, current-HEAD
  evidence, and explicit completion.
- Host isolation is outside the harness. The harness can declare and validate
  scope; only the host can actually prevent a process from escaping it.
- Never add a bypass for evidence, approval, authority, scope, or validation.
- Every enforcement point carries a bite test: a case that constructs the
  forbidden state and asserts the refusal code and its one legal next action.
  A gate no test can make fail is not a gate. Weakening a bite test to make a
  change pass is never the fix.
- Complexity must earn its place through real use. New surface without an owner
  in `release/surface-inventory.md` fails the build.

## Working rules

1. Trace the real implementation and tests before choosing an abstraction.
2. Plan non-trivial work as a change under `.specd/` rather than an ad-hoc edit.
3. Reuse existing conventions; avoid speculative compatibility.
4. Test the narrow behavior first, then the affected suite.
5. Report assumptions, deferred complexity, and any mismatch between docs,
   source, and tests.

## Checks

```bash
make ci     # the full gate: formatting, vet, empty require set, advisories,
            # release contracts, benchmark panic-freedom, fuzz bursts, the
            # raced suite, and the --version/--help smoke
make docs   # regenerate the operations document after a registry change
make hooks  # opt in to .githooks (pre-commit formats, pre-push vets and tests)
```

`make ci` is the local projection of `.github/workflows/ci.yml`; if a gate is
added to one, it is added to the other in the same commit. On a platform or
shell without `make`, run the same commands directly:

```bash
gofmt -l .
go vet ./...
go test ./... -race -count=1
go run ./cmd/specd --version && go run ./cmd/specd --help
test -z "$(go list -m all | tail -n +2)"
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
sh release/tag-contract.sh --self-check
sh release/prepare.sh --self-check
go test ./... -run '^$' -bench . -benchtime 1x

SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
```

`make vuln` needs network; `make check-offline` is `make check` without it.
`go.mod` having an empty require set is a release gate. Run the focused check
during iteration, the full suite before handing off. Do not add a framework or
dependency for a one-off check.
