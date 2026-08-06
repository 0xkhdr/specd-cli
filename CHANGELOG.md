# Changelog

Notable changes to specd. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public surface may break on any minor bump.
The boundary each release stands behind is
[`release/release-decision.md`](release/release-decision.md), not this file.
This file records what changed; that one records what has been proven.

## [Unreleased]

### Fixed

- A change directory with no `state.json` — what an abandoned or half-created
  change leaves behind — refused with a raw filesystem error whose recovery
  action was `run specd status <change>`, the command that had just failed.
  Every operation that loads state now refuses `check_state` with one legal next
  action, because the refusal moved into the single reader of the state file
  instead of being restated by one caller.
- The maturity registry was decoration for every claim no summary sentence
  named: upgrading a gated platform to `proven` — a published claim with no
  observation behind it — kept the whole suite green. Platform levels are now
  checked against the supported tier stated in `release/release-decision.md`,
  and every platform claim must carry the date of the raced-suite observation
  recorded there, so a level or a date cannot move alone.

### Added

- Phase 4 completes the hardening adoption set: `ARCHITECTURE.md` maps code
  layers and every design rule to its enforcing test; a typed core registry
  projects dated maturity and assurance claims into agent guidance and status
  reports with a release-gate bite test; contributor policy now includes the
  merge boundary, agent-authored change responsibility, three extension
  cookbooks, and real failure-shaped gotchas.
- Phase 3 hardening adds an independent test-local runtime conformance model
  over all fourteen journeys and every executable operation, four executable
  growth benchmarks with dated results in `release/scale.md`, and a release-PR
  flow where merging `release/<version>` mechanically creates an immutable
  annotated tag. Publication remains a retry-only manual dispatch against that
  existing tag.
- Phase 2 release proof: every foundation invariant has an actionable bite
  assertion; native fuzz targets cover path containment, ledger replay, state
  decode, and all planning parser families; task transitions have a fixed-seed
  property check; plan output has an explicit byte golden; and
  `release/gate-limits.md` records every gate's blind spots. CI and `make ci`
  run bounded fuzz bursts for the three highest-risk boundaries.
- One command reproduces the gate: `make ci` is the local projection of
  `.github/workflows/ci.yml` — formatting, `go vet`, the empty-require-set
  check, a `govulncheck` scan, the raced suite, and the `--version`/`--help`
  smoke. A gate added to one is added to the other in the same commit. CI still
  calls the commands directly, so a failure names the gate and the
  `windows-latest` leg needs no `make`. `make hooks` opts in to `.githooks`,
  where `pre-commit` formats staged Go files and re-stages them and `pre-push`
  runs `go vet` and the suite; both files list where they deliberately diverge
  from CI. Installing them stays a consented action — nothing else writes a
  contributor's git config.
- `release/tag-contract.sh --self-check` now runs on every push and pull
  request instead of only inside the release workflow on a tag push. The gate
  that decides whether a tag may consume a release was previously discovered
  broken while cutting one.
- `govulncheck` runs as its own CI job, so an advisory reachable from called
  code is distinguishable from a test failure at a glance. Zero runtime
  dependencies is not zero advisories: specd links the standard library and is
  built by a pinned toolchain. It runs via `go run` and leaves the require set
  empty. `SECURITY.md` states the three legal resolutions for a finding and
  refuses a fourth.
- Every CI and release job now carries `timeout-minutes`, and
  `.github/CODEOWNERS` routes review for the paths the suite trusts rather than
  checks.

- Contention between callers is now proven rather than argued.
  `TestConcurrentCallersOneRoot` races six real `specd` processes against one
  root: a contested task transition elects exactly one caller and every loser
  fails closed on a named refusal carrying one legal next action, and racing
  appends to the shared history ledger all survive a clean replay with no lost,
  duplicated, or torn record. Real processes rather than goroutines, because the
  in-process mutex in `internal/core/lock` would satisfy an in-process race
  without the file lock ever being exercised. Limitation 2 of
  [`release/release-decision.md`](release/release-decision.md) is narrowed
  accordingly; driving the loop end to end from two callers at once is still not
  claimed.

### Removed

- The second renderer set in `internal/cmd` is gone: `RenderCheck`,
  `RenderStatusText`, `RenderStatusJSON`, `RenderContextJSON`,
  `RenderReportText`, and `RenderReportJSON` had no caller outside their own
  tests, because every shipped surface already renders the one agent envelope.
  The tests now assert against that envelope, so they exercise what users and
  agents actually read.
- `StatusResult.Production` and its `StatusProduction` type. Profile, policy
  digest, assurance, review approvability, and deferred-domain eligibility
  reached no surface from `status`; `report --kind status` and
  `report --kind review` are the one owner of those projections. `status` no
  longer builds a review packet or a friction projection it never emitted.

### Changed

- `cmd.Start` and `cmd.Complete` take the actor directly instead of a
  single-field options struct, and `cmd.Start`, `cmd.Complete`, and
  `cmd.Archive` no longer restate the empty-actor check. `core.StartTaskAttempt`,
  `core.CompleteTask`, and `core.Archive` already refuse an empty actor before
  any effect, and their refusal carries a code and one legal next action where
  the duplicated check returned a bare error.
- Standard library first: `sort` is gone in favour of `slices` and `cmp`
  (`slices.Sort`, `slices.SortFunc`, `slices.SortStableFunc`, `cmp.Or`),
  hand-rolled membership loops are `slices.Contains`/`slices.ContainsFunc`,
  clone idioms are `slices.Clone`, and first-error selection is `cmp.Or`.
- `record.AttemptIdentity`, `core.DecodeTaskActivity`,
  `core.ApprovalStatusProjection`, and `evidence.KnownClass` are unexported;
  each was only ever called from inside its own package.

### Fixed

- Windows device names such as `con`, `nul`, `com1`, and `lpt9` are refused as
  managed path segments on every platform, keeping accepted names portable.
- The empty-require-set check in CI passed when `go list -m all` failed. A
  failing `go list` prints nothing, and nothing read as "no dependencies", so a
  `go.mod` with a require line and no matching `go.sum` entry — the state a
  freshly added dependency is in — was accepted. The check now asserts the exit
  status before the output, in CI and in `make deps-empty` alike. Found by
  deliberately breaking the gate rather than by trusting it.

## [0.3.0] — 2026-08-02

### Added

- A tagged build now decides whether the tag is allowed to consume a release
  before it spends one. `release/tag-contract.sh` checks the two clauses
  `CONTRIBUTING.md` states — the tag is annotated, and `CHANGELOG.md` has a
  section for the version it names — and a `contract` job runs it ahead of the
  gates. A lightweight tag was previously refused nowhere, and a missing
  changelog section was refused only in the final publish step, after four
  platforms had run the suite, five binaries had been built, and a provenance
  attestation had been recorded.

### Changed

- The release workflow was changed as `release-contract-gate`, a change planned,
  approved, executed, verified, and archived through specd's own loop. The
  machinery was previously the one part of this repository that had never been
  through its own harness. Cutting a release is still not: see limitation 6 of
  [`release/release-decision.md`](release/release-decision.md).
- Two harness defects were found by doing that and are recorded as limitations 7
  and 8: an attempt is bound to one commit and only `complete` releases it, so a
  plan left uncommitted when its first task starts strands that task on a
  refusal naming an action no operation can perform; and a declared file list
  written with commas instead of semicolons is accepted at plan time and fails
  only when the task starts.

### Install

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.3.0
```

## [0.2.0] — 2026-08-01

Three operating systems and two architectures, all observed rather than
inferred. This release also carries the first change specd planned, approved,
executed, verified, and archived through its own loop in this repository.

### Added

- Windows and `linux/arm64` are supported platforms for the suite and for all
  fourteen release journeys, and both gate every release. Windows previously
  failed 254 tests and was not in the matrix at all; `linux/arm64` had never
  been run. No change has been driven through the loop by hand on either, so
  the end-to-end guarantee stays limited to `linux/amd64` — see
  [`release/release-decision.md`](release/release-decision.md).
- A tagged build publishes binaries for `linux/amd64`, `linux/arm64`,
  `darwin/arm64`, `darwin/amd64`, and `windows/amd64`, each covered by a green
  run on its own platform.
- `.gitattributes` pins the source to LF, so a Windows checkout is the same
  bytes as every other one.

### Fixed

- Every managed write on Windows failed with `Access is denied`: the write
  ended in a directory flush, which Windows refuses on a directory handle.
  There is one owner of that call now, and it is a no-op on Windows, where
  the rename's durability rests on NTFS metadata journaling instead.
- `archive` could never succeed on Windows: the change lock lived inside the
  folder `archive` renames, and Windows refuses to rename a directory holding
  an open handle. The lock now sits beside the folder as `changes/<name>.lock`.
- Operations that act under an actor identity refused on Windows, which sets
  `USERNAME` rather than `USER`.
- The human route could not be derived on a Windows console, so the human gate
  was unreachable there. It is now derived through `GetConsoleMode`, the
  platform's equivalent of the termios probe the Unix build uses.
- Verification process trees are terminated through a job object rather than
  `taskkill`, whose exit status cannot distinguish a descendant it failed to
  kill from one that had already exited.

### Changed

- A change's lock moved from `.specd/changes/<name>/.lock` to
  `.specd/changes/<name>.lock`. Locking the folder that `archive` renames is
  what made `archive` impossible on Windows. Nothing reads a lock file, so
  upgrading needs no migration, but a root created before this release keeps a
  `.specd/.gitignore` that names the old location: add `changes/*.lock` to it,
  or the new lock files show up as untracked. They are never treated as
  content — any `.specd/**.lock` is ignored by scope and dirty-worktree checks.
- The documentation is reorganized around audiences: `README.md` is a product
  and boundary overview, and `docs/README.md` is the one map into the rest.
  This was authored as the `docs-navigation` change and driven through the
  loop, which is also what retires limitations 5 and 6 of the previous release
  decision.

### Install

```bash
go install github.com/0xkhdr/specd-cli/cmd/specd@v0.2.0
```

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

[Unreleased]: https://github.com/0xkhdr/specd-cli/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.3.0
[0.2.0]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.2.0
[0.1.1]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.1.1
[0.1.0]: https://github.com/0xkhdr/specd-cli/releases/tag/v0.1.0
