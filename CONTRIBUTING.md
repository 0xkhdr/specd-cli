# Contributing

The practical build, test, and extension guide is
[`docs/contributing.md`](docs/contributing.md).

[`AGENTS.md`](AGENTS.md) is the binding contributor and agent guide. Read it
before changing anything — it defines the vocabulary, the design rules, and what
the release gates refuse.

## Before you open a pull request

```bash
go test ./... -race -count=1
go vet ./...
gofmt -l .
```

All three must be clean. `gofmt -l .` must print nothing.

specd is standard library only. `go.mod` having an empty require set is a
release gate, so a pull request that adds a dependency fails the build by
design. If you believe one is genuinely needed, open an issue arguing the case
before writing the code.

New exported surface needs an owner in
[`release/surface-inventory.md`](release/surface-inventory.md): every exported
symbol maps to one exercised journey, one protected invariant, or one named
external contract. Unowned surface fails the build.

After a change to the operation registry, regenerate the operations document:

```bash
SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
```

## Commits and pull requests

One logical change per pull request. Explain what behavior changed and how you
verified it — a reviewer should not have to guess which command you ran.

If a change alters what the project claims to have proven — a platform, a
guarantee, a boundary — say so in
[`release/release-decision.md`](release/release-decision.md) in the same pull
request. That file is the boundary this project stands behind, and it is meant
to be edited honestly rather than kept flattering.

## Reporting security issues

Do not open a public issue. See [`SECURITY.md`](SECURITY.md).
