# Changelog

Notable changes to specd. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public surface may break on any minor bump.
The boundary each release stands behind is
[`release/release-decision.md`](release/release-decision.md), not this file.
This file records what changed; that one records what has been proven.

## [Unreleased]

## [0.1.0] — 2026-08-01

First release.

### Added

- The base loop, end to end: `init`, `new`, `check`, `approve`, `status`,
  `next`, `context`, `start`, `verify`, `complete`, `sync`, `archive`.
- `review`, `report`, `friction` — the production surface, opt-in and labelled
  experimental. They weaken no default gate.
- `--json` on every operation: one stable machine-readable envelope, with
  `next.kind` as an agent's control flow.
- `specd --help` renders the operation palette from the operation registry;
  `specd --version` reports the release tag in a published binary and the
  commit it was built from in a local build.
- CI on `ubuntu-latest`, `macos-latest`, and `windows-latest`: `go vet`,
  `go test -race`, formatting, and the empty require set. A tagged build
  re-runs the suite on every target platform, then publishes binaries for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and
  `windows/amd64` with `SHA256SUMS` and a build provenance attestation.
- `SECURITY.md` — what specd defends, what it does not, and how to report.

### Guarantees this release stands behind

- Verification evidence is an observation pinned to current HEAD; it never
  completes a task by itself.
- An agent cannot approve its own plan. The human route is derived from a
  termios ioctl, and there is no bypass flag.
- A task's declared file scope is enforced, and git-ignored files count.
- Managed state is written old-or-new under revision guards.
- Every refusal fails closed carrying exactly one legal next action.
- No LLM and no network in any validation, state, graph, evidence, or report
  path.

### Known limitations

Stated in full in [`release/release-decision.md`](release/release-decision.md).
In short: one traversal is one traversal — the loop is proven by fourteen
replayed journeys and one real two-task change, in one root. It is not proven
at scale, across concurrent callers, or over long-lived changes.

### Install

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.1.0
```

[Unreleased]: https://github.com/0xkhdr/specd-cli/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.1.0
