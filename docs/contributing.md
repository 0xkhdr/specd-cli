# Contributing

Use this guide to build, test, document, and extend specd without weakening its
enforced guarantees.

Read [`../AGENTS.md`](../AGENTS.md) first — it is the binding workspace guide.
This page is the practical version of it.

## Build and test

```bash
go build -o specd ./cmd/specd
go test ./... -race -count=1
go vet ./...
```

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
   a second one, you are about to create drift.
4. **Fail closed.** Stale, malformed, future, ambiguous, unsafe, or unauthorized
   input stops. Every refusal carries exactly one legal next action.
5. **Keep the four concepts separate**: activity, readiness, evidence, approval.
6. **Be honest about assurance.** The harness validates scope; only a host
   contains a process. Don't relabel `advisory` as enforced.
7. **No speculative surface.** No interface with one implementation, no config
   for a value that never changes, no field reserved for a deferred domain.
   `TestSurfaceOwnership` enforces this, and it is stricter than review.

## Adding an operation

The registry is the source of truth; everything else projects from it.

1. **Declare it** in `internal/core/operations.go` — id, summary, actor, effect,
   lifecycles, arguments, flags, exits, result type, `AgentVisible`,
   `Executable`, example. An undeclared field fails registry validation rather
   than defaulting to something wider.
2. **Bind one handler** in the `handlers` map in `internal/cmd/dispatch.go`.
   Exactly one per executable id.
3. **Project the result** into the agent envelope. If the result type is not
   projected, the command is registered, documented, and unreachable — this
   actually shipped once, and it is why reachability is tested rather than
   assumed.
4. **Regenerate the docs**:

   ```bash
   SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
   ```

   Never hand-edit `docs/operations.md`; the parity check compares bytes.
5. **Add a surface-inventory row** in `release/surface-inventory.md` mapping it
   to a journey or an invariant.
6. **Cover it with a journey** if it is user-reachable, in
   `internal/integration/release_journeys_test.go`.
7. **Update the hand-written docs** only where the operation changes a workflow
   or guarantee. The README links to the generated operation reference instead
   of maintaining a second command table.

If the operation is human-only, `AgentVisible` is false and there is no
agent-callable form. Do not add a flag that reveals one.

## Adding a gate

Planning gates live in `internal/core/gates/planning.go`, production gates in
`production.go`, both registered in `registry.go`. A gate returns findings with
a code, a `file:line` location, a message, and a `fix:` — the fix is not
optional, because a finding a caller cannot act on is noise.

Changing the registry changes its version, which makes existing approvals stale
(`registry_version_changed`). That is correct behavior, and it means gate
changes are not free — they cost every in-flight change a re-approval.

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

## Before you hand off

- the narrow test for what you changed, then `go test ./... -race -count=1`;
- `go vet ./...`;
- `gofmt` clean (the release gate checks it without shelling out, so run it
  yourself first);
- generated files regenerated, not edited;
- `release/surface-inventory.md` updated if you added or removed surface;
- assumptions, deliberate simplifications, and any mismatch between docs, source,
  and tests reported rather than silently resolved.

A deliberate shortcut with a known ceiling gets a `ponytail:` comment naming the
ceiling and the upgrade trigger. There are a few in the tree already; follow the
shape.

Report any mismatch among docs, source, tests, security policy, and release
evidence. Do not silently choose the more convenient claim.
