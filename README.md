# specd

A spec-driven coding harness. It moves process enforcement out of an AI agent's
context window into a deterministic, local, tool-gated pipeline: plan a change
in Markdown, get it approved by a human, then execute it one task at a time
against recorded evidence.

> The agent reasons. The harness enforces.

Go, standard library only, zero runtime dependencies, one static binary.

## Status

Released and young. `0.x` is not a formality: the public surface may break on
any minor bump.

The base loop is implemented and proven end to end by fourteen replayed
journeys and by one real change through the whole loop — one change, in one
root. It is not proven at scale, across concurrent callers, or over long-lived
changes. Read [`release/release-decision.md`](release/release-decision.md) for
exactly what has been proven and what has not; it is the boundary this project
stands behind.

## Install

Go 1.26 or newer:

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.1.1
specd --version
```

Or build from source:

```bash
git clone https://github.com/0xkhdr/specd-cli
cd specd-cli
go build -o specd ./cmd/specd
```

Each tagged release also publishes one `linux/amd64` binary, with `SHA256SUMS`
and a build provenance attestation. Verify a download before running it:

```bash
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify specd_linux_amd64 --repo 0xkhdr/specd-cli
```

The suite runs on Linux and macOS before that artifact is published. Windows is
unsupported — it was run on 2026-08-01 and failed 254 tests on line-ending and
path-separator assumptions, so it is a port rather than a bug list. See
[`release/release-decision.md`](release/release-decision.md) for what each
platform tier does and does not establish. There is no installer script and no
package.

## The loop

```text
init → new → author proposal/delta/design/tasks → check → human approve
→ next → context → start → edit declared files → verify → complete
→ human sync → sync → archive
```

Two of those steps are human, not agent: `approve` authorizes the plan, and
`sync` authorizes accepted truth. The harness derives the human route from a
controlling terminal, so an agent cannot pass either gate. There is no bypass
flag.

Getting started, on an empty project:

```bash
specd init                       # create or adopt the managed .specd root
specd new add-dark-mode          # create the change and its planning artifacts
# author proposal.md, the delta spec, design.md, and tasks.md
specd check add-dark-mode        # run the planning gates
specd approve add-dark-mode --approver you --reason "plan reviewed"
specd next add-dark-mode         # the ready frontier
specd context add-dark-mode T1   # the bounded read context for one task
specd start add-dark-mode T1 --revision 2      # bind a baseline, open the attempt
# edit only the files that task declares
specd verify add-dark-mode T1 <attempt>        # record evidence at current HEAD
specd complete add-dark-mode T1 --revision 3   # consume the evidence, close the task
specd sync add-dark-mode --approver you --reason "behavior accepted"
specd archive add-dark-mode
```

`start` and `complete` take the revision you observed from `status`: if the
change moved underneath you, they refuse instead of acting on stale state.
`verify` takes the attempt id `start` returned.

## Commands

| command | what it does |
| --- | --- |
| `init` | Create or adopt the managed `.specd` root and install the agent guidance file. |
| `new` | Create a change with its planning artifacts and state. |
| `check` | Run planning gates over the change and report findings. |
| `approve` | Record human approval of the current planning artifacts. |
| `status` | Report lifecycle, approval, readiness, and next action for a change. |
| `next` | Project the ready task frontier or the single blocking action. |
| `context` | Assemble the bounded read context for exactly one task. |
| `start` | Bind a clean Git baseline and open one task attempt. |
| `verify` | Run the task's declared verification and record evidence at current HEAD. |
| `complete` | Consume applicable passing evidence and close the task. |
| `review` | Record or project one separate reviewer verdict for a task. |
| `sync` | Reconcile approved deltas into accepted specs. |
| `archive` | Validate and move a reconciled change into the archive. |
| `report` | Project one of the four canonical read-only reports. |
| `friction` | Record one observation that a deferred domain blocked real work. |

Every flag, exit code, and allowed lifecycle lives in
[`docs/operations.md`](docs/operations.md), which is generated from the
operation registry and byte-checked against it by the release gate. It is the
source of truth; nothing else restates a flag.

Add `--json` to any operation for the stable machine-readable envelope an agent
reads.

`specd --help` prints this palette from the same registry, and `specd --version`
reports the build. Neither is an operation: they resolve no root and write
nothing.

## Documentation

Start at [`docs/`](docs/README.md).

| doc | what it gives you |
| --- | --- |
| [`docs/getting-started.md`](docs/getting-started.md) | one change from `init` to `archive`, on a real project |
| [`docs/agent-setup.md`](docs/agent-setup.md) | JSON envelope, operation palette, generated guidance, host assurance |
| [`docs/concepts.md`](docs/concepts.md) | the model: root, spec, change, lifecycle, approval, evidence, authority |
| [`docs/the-loop.md`](docs/the-loop.md) | `next` → `context` → `start` → `verify` → `complete`, in depth |
| [`docs/approval-and-evidence.md`](docs/approval-and-evidence.md) | why verify isn't completion, why an agent can't self-approve |
| [`docs/layout.md`](docs/layout.md) | the `.specd/` on-disk format and who owns each file |
| [`docs/troubleshooting.md`](docs/troubleshooting.md) | every refusal code, what it means, and the one legal next action |
| [`docs/operations.md`](docs/operations.md) | every command, flag, exit code — generated from the registry |
| [`docs/contributing.md`](docs/contributing.md) | build, test, release gates, how to add an operation |

## What it guarantees

- **Evidence is not completion.** `verify` records an observation pinned to
  current HEAD. `complete` is a separate harness-owned transition that consumes
  applicable passing evidence. No free-text claim closes a task.
- **An agent cannot approve its own plan.** Approval is a human act, and editing
  an approved artifact makes the approval stale.
- **Declared scope is enforced.** A task names the files it may touch; a diff
  outside them is refused. Git-ignored files count, deliberately.
- **State is harness-owned.** `.specd/` state, history, evidence, and task
  markers are written atomically with revision guards and are never hand-edited.
- **Refusals are actionable.** Every refusal fails closed and carries exactly
  one legal next action.
- **No LLM and no network** in any validation, state, graph, evidence, or report
  path.

## Repository layout

| path | what it holds |
| --- | --- |
| `cmd/specd/`, `internal/` | the implementation |
| `docs/` | documentation; `operations.md` is generated |
| `release/` | the release decision and the surface ownership inventory |
| `.github/workflows/` | the CI gates and the tagged-release build |
| `.specd/` | specd's own planning root — it dogfoods itself |
| `AGENTS.md` | the contributor and agent guide for this workspace |
| [`SECURITY.md`](SECURITY.md) | what specd defends, what it does not, and how to report |
| [`CHANGELOG.md`](CHANGELOG.md) | what changed in each release |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | how to open a pull request that passes the gates |

## Contributing

```bash
go test ./... -race -count=1
go vet ./...
gofmt -l .
```

Standard library only — `go.mod` having an empty require set is a release gate.
Full guide: [`docs/contributing.md`](docs/contributing.md) and
[`CONTRIBUTING.md`](CONTRIBUTING.md). Read
[`AGENTS.md`](AGENTS.md) before changing anything, and
[`release/surface-inventory.md`](release/surface-inventory.md) before adding a
surface: every exported symbol maps to one journey or one invariant, and
unowned surface fails the build.

## License

MIT. See [`LICENSE`](LICENSE).
