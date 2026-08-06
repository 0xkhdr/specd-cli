# specd

specd is a local, spec-driven coding harness. You describe a change in
Markdown, a human approves the plan, and an agent implements one declared task
at a time. The binary enforces lifecycle, scope, evidence, and completion
instead of relying on an agent to remember the process.

> The agent reasons. The harness enforces.

One Go static binary. Standard library only. No runtime dependencies, LLM
calls, network calls, daemon, or telemetry in the deterministic pipeline.

## Project status

specd is released at `v0.3.0` and remains a young `0.x` project. The base loop
is proven end to end on linux/amd64. On linux/arm64, macOS, and Windows the
suite and all fourteen release journeys are green and gate every release, but
no change has been driven through the loop by hand there. The production
profile is experimental; scale and long-lived changes are not yet proven.
Contention between concurrent callers against one root is proven — one caller
wins a contested transition, losers fail closed, and the shared history ledger
replays clean — but driving the loop end to end from two callers at once is
still not claimed.

Before production use, read the exact [release boundary](release/release-decision.md)
and [security model](SECURITY.md). Host assurance is `advisory` unless the host
provides containment: specd detects out-of-scope writes but does not sandbox the
process that made them.

## Install

Requires Go 1.26 or newer:

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.3.0
specd --version
```

`go install` writes to `$(go env GOPATH)/bin`. If `specd --version` reports
`command not found`, that directory is not on your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Or build this checkout:

```bash
go build -o specd ./cmd/specd
./specd --version
```

Tagged releases also publish a binary for linux/amd64, linux/arm64,
darwin/arm64, darwin/amd64, and windows/amd64, with checksums and build
provenance:

```bash
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify specd_linux_amd64 --repo 0xkhdr/specd-cli
```

## How it works

```text
init → new → author proposal/delta/design/tasks → check → human approve
→ next → context → start → edit declared files → verify → complete
→ human sync → archive
```

`approve` and `sync` require a human at a controlling terminal. A passing
`verify` records evidence; it does not complete the task. `complete` consumes
applicable evidence at the current Git HEAD.

Start a change:

```bash
specd init
specd new add-dark-mode --capability interface
# Author the files under .specd/changes/add-dark-mode/.
specd check add-dark-mode
```

The next action is always printed in the result. Follow the complete,
reproducible walkthrough in [Getting started](docs/getting-started.md).

## Documentation

Use the [documentation map](docs/README.md) to choose a path:

- evaluate and adopt specd;
- run the task loop;
- integrate an AI agent;
- troubleshoot a refusal;
- contribute to the codebase.

Exact commands, flags, lifecycle constraints, and exits live in the generated
[operation reference](docs/operations.md). It is built from the same registry
as `specd --help`.

## Guarantees

- Human approval and accepted-truth synchronization have no agent bypass.
- Evidence is bound to the change, task, attempt, command, approval, revision,
  and current Git HEAD.
- Declared file scope is validated, including Git-ignored files.
- Managed state writes are atomic and revision-guarded.
- Stale, corrupt, ambiguous, or unauthorized state fails closed.
- Every refusal returns one legal next action.

These are validation guarantees, not process containment. See the
[release decision](release/release-decision.md) for the evidence supporting
each claim.

## Contributing

Read [AGENTS.md](AGENTS.md) and the [contributor guide](docs/contributing.md)
before changing the project. The full handoff check is one command, and it is
the local projection of `.github/workflows/ci.yml`:

```bash
make ci
```

New runtime dependencies and unowned public surface fail the release gate.

## License

MIT. See [LICENSE](LICENSE).
