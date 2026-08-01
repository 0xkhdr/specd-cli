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

## Versioning and releases

Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is `0`, the public surface may break on any minor bump.

A release is one annotated tag named `vMAJOR.MINOR.PATCH`, and it is complete
only when all of these exist:

- the annotated tag, on a commit whose gating platforms are green;
- a `CHANGELOG.md` section for it, with the `go install` line;
- a GitHub release whose body is that section;
- binaries for every platform the release decision claims, with `SHA256SUMS`
  and a build provenance attestation.

A published version is immutable. Tags are never deleted, moved, or reused —
`proxy.golang.org` caches every version permanently, so deleting a tag cannot
unpublish it; it only makes the tag and the proxy disagree. A release that
should not be used is superseded by the next one and said to be superseded in
its `CHANGELOG.md` entry, as `0.1.0` is. A release that is genuinely unsafe to
build against is withdrawn with a `retract` directive in `go.mod`, which is the
only supported way to take a version back.

## Reporting security issues

Do not open a public issue. See [`SECURITY.md`](SECURITY.md).
