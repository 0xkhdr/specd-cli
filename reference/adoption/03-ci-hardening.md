# 03 — CI hardening

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P12](../patterns.md#p12--every-rule-names-its-enforcement-and-forbids-the-escape-hatch) | 1 | small | low | applied 2026-08-06 |

## Why

specd's CI is already better than buzz's in the things that matter most —
pinned action SHAs, workflow-level least privilege, a four-platform matrix, and
a release workflow that re-runs the full gate against the tag rather than
trusting the branch build. Three things buzz does that specd does not:

1. **`timeout-minutes` on every job.** buzz sets 2–45 minutes per job. specd
   sets none, so a hung `go test` consumes the full six-hour default on four
   runners.
2. **A dedicated security job.** buzz runs `cargo-deny check` in its own job so
   a dependency-policy failure is distinguishable from a test failure. specd's
   equivalent is [adoption 04](04-supply-chain.md).
3. **Release machinery is tested like code.** buzz runs
   `test-release-ref-contract.sh` and three other contract scripts in CI
   unconditionally, on every PR, regardless of path filters. specd has
   `release/tag-contract.sh --self-check`, which runs **only inside the release
   workflow, only on a tag push**. A change that breaks the tag contract is
   discovered at release time.

Item 3 is the real gap. The contract script is the gate that decides whether a
tag may consume a release; discovering it is broken while cutting a release is
the worst possible time.

## Change set

### 3.1 Per-job timeouts

In `.github/workflows/ci.yml` and `.github/workflows/release.yml`, add
`timeout-minutes` to every job, sized to roughly twice the observed runtime:

| Job | Suggested |
| --- | --- |
| `ci.repo` | 5 |
| `ci.gates` | 20 |
| `release.contract` | 5 |
| `release.gates` | 20 |
| `release.publish` | 20 |

Add the reason as a comment, in the register the workflow already uses: a job
that has stopped making progress should fail fast rather than hold four runners
for the default six hours.

### 3.2 Run the tag contract on every push and PR

Add to the `repo` job in `.github/workflows/ci.yml`, after the gofmt step:

```yaml
      # The release workflow's first gate, exercised on every change rather
      # than only when a tag is pushed. `--self-check` is the script asserting
      # its own accept/reject behavior against synthetic inputs, so this runs
      # with no tag present and no release in flight. Finding this broken while
      # cutting a release is finding out too late.
      - name: release tag contract self-check
        run: sh release/tag-contract.sh --self-check
```

If `--self-check` currently requires a tag or a network call, that is the defect
to fix first: it must be a pure, offline assertion of the script's own logic,
the way buzz's `test-release-ref-contract.sh` is.

### 3.3 State the workflow/Makefile parity obligation

Add a comment above the `gates` job:

```yaml
    # `make ci` is the local projection of this job list. A gate added here is
    # added there in the same commit. CI calls the underlying commands directly
    # rather than invoking make, so a failure names the gate and the
    # windows-latest leg needs no make. See reference/adoption/01.
```

## Acceptance

```bash
sh release/tag-contract.sh --self-check   # passes offline, with no tag checked out
```

Push a branch and confirm: every job reports a timeout in its settings, the
contract self-check appears in the `repo` job, and total wall time is unchanged
within noise.

## Do not

- **Do not add path filtering.** buzz filters because it has five surfaces and
  45-minute jobs; specd has one module and a suite that finishes in minutes.
  Filtering buys nothing and adds a way for a gate to be skipped when it should
  have run — the opposite of fail-closed.
- **Do not remove a matrix leg to save time.** `release/release-decision.md`
  binds the platform claim to the legs that actually run; dropping a leg narrows
  a published claim and belongs in that document first. The workflow already
  says this in a comment — keep it.
- **Do not widen `permissions:`** on any CI job. `contents: read` is correct;
  only `release.publish` needs more, and it already declares exactly what it
  needs.
- **Do not unpin an action to a tag.** `SECURITY.md` publishes SHA pinning as a
  defense: "a compromised upstream tag cannot silently enter a release build."

## Deferred

Job-level caching (`actions/cache` for the Go build cache). specd's suite is
fast enough that a cache adds a stale-state failure mode for a small saving.
Revisit if `ci.gates` exceeds ten minutes on any leg.
