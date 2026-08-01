# Changelog

Notable changes to specd. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public surface may break on any minor bump.
The boundary each release stands behind is
[`release/release-decision.md`](release/release-decision.md), not this file.
This file records what changed; that one records what has been proven.

## [Unreleased]

## [0.1.1] — 2026-08-01

The first installable release. `0.1.0` is tagged but published no binaries and
carries a platform claim that its own CI run disproved minutes later; use this
one.

### Fixed

- Tests no longer disagree with the harness about what a root is. Nineteen
  tests across five packages compared against a raw `t.TempDir()` while every
  production entry point canonicalizes the selected root through
  `filepath.EvalSymlinks`. The two diverge wherever the temporary directory
  sits under a symlink — `/var/folders` on macOS — so the suite failed there
  while the harness was behaving correctly. Test roots are now canonicalized
  the same way production resolves them.

### Changed

- Supported platforms are stated as three tiers rather than one line:
  linux/amd64 supported, macOS tests-green but the loop not driven there, and
  Windows unsupported because it was run and failed. See
  [`release/release-decision.md`](release/release-decision.md).
- CI runs Linux and macOS. `windows-latest` was removed after failing 254 tests
  across 14 packages on line-ending and path-separator assumptions — a port,
  not a defect list. The reason is recorded in the release decision so removing
  the runner does not erase the result.
- A tagged build publishes one `linux/amd64` binary, matching the boundary the
  release decision actually claims, rather than five binaries for platforms it
  does not.

### Install

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.1.1
```

## [0.1.0] — 2026-08-01

First release. Superseded by `0.1.1`: no binaries were published, and its
`release/release-decision.md` describes macOS and Windows as gated-but-unobserved
when the run that observed them had already failed.

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
- CI: `go vet`, `go test -race`, formatting, and the empty require set. A
  tagged build re-runs the suite on every gating platform, then publishes a
  binary with `SHA256SUMS` and a build provenance attestation.
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

[Unreleased]: https://github.com/0xkhdr/specd-cli/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.1.1
[0.1.0]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.1.0
