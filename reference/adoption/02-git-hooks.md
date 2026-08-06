# 02 — Tiered git hooks, no dependency

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P2](../patterns.md#p2--cheap-and-auto-fixing-at-commit-fast-and-verifying-at-push) | 1 | small | low | not applied |

## Why

buzz's `lefthook.yml` encodes a cost gradient that is worth copying exactly:

- **pre-commit** — formatters only, in parallel, with `stage_fixed: true`. A
  fixable problem is fixed and re-staged rather than rejected. Only unfixable
  lint blocks the commit.
- **commit-msg** — one idempotent trailer rewrite.
- **pre-push** — the fast verifying suites, explicitly with "no overlap with
  pre-commit". "Builds are CI-only."

specd has no hooks. The cheapest failure in this repository — pushing a branch
that fails CI on `gofmt` — is currently unprevented, and it costs a full
four-platform matrix run to discover.

## Do not use lefthook

Same reasoning as adoption 01: a third-party binary to run `gofmt`. Git's own
`core.hooksPath` (git ≥ 2.9) points at a checked-in directory and needs nothing
installed. It works on all four platforms specd claims, including
`windows-latest`, where git ships its own `sh`.

## Change set

Create `.githooks/pre-commit`:

```sh
#!/bin/sh
# Cheap and auto-fixing. Formats staged Go files and re-stages them, mirroring
# lefthook's `stage_fixed`. It never rejects work it can repair itself.
#
# Deliberate deviation from .github/workflows/ci.yml: this hook formats only
# staged files, while CI checks the whole tree. A commit that leaves an
# unrelated file unformatted is caught at push and in CI, not here.
set -e

staged="$(git diff --cached --name-only --diff-filter=ACM -- '*.go')"
[ -n "$staged" ] || exit 0

# shellcheck disable=SC2086
gofmt -l -w $staged
# shellcheck disable=SC2086
git add $staged
```

Create `.githooks/pre-push`:

```sh
#!/bin/sh
# Fast and verifying. No overlap with pre-commit: formatting was already fixed
# there, so this runs the checks that catch what formatting cannot.
#
# -race is deliberately omitted here and required in CI. A raced test takes
# minutes locally on some hosts; the four-platform matrix is where the race
# detector earns its cost. This is a stated deviation, not an oversight.
set -e

go vet ./...
go test ./... -count=1
```

Make both executable and commit the mode bit:

```bash
chmod +x .githooks/pre-commit .githooks/pre-push
git update-index --chmod=+x .githooks/pre-commit .githooks/pre-push
```

Wire installation into the `Makefile` from adoption 01 (`make hooks` →
`git config core.hooksPath .githooks`) and document it in
`docs/contributing.md` and `AGENTS.md` §Checks.

## Header comment obligation

Copy buzz's most under-appreciated practice: `lefthook.yml` opens with a comment
block listing every place the hooks intentionally diverge from CI, each with a
reason, closing with "Deliberate — accepted, not worked around." Both hook files
above carry that block. Any future divergence gets a line there in the same
commit, or the hooks become a second, drifting source of truth — the exact
failure `AGENTS.md` names under "One validation boundary."

## Acceptance

```bash
make hooks
git config --get core.hooksPath          # .githooks

# pre-commit repairs rather than rejects
printf 'package core\n\nfunc  x() {}\n' > internal/core/hooktest.go
git add internal/core/hooktest.go && git commit -m "test"
gofmt -l internal/core/hooktest.go       # empty: the hook fixed and re-staged it
git reset --hard HEAD~1

# pre-push refuses a failing tree
# (introduce a vet failure, attempt a push to a scratch branch, confirm refusal)
```

## Do not

- Do not run `-race` in `pre-push`. It moves the hook from "fast" to "skipped",
  and a skipped hook enforces nothing.
- Do not add a `commit-msg` hook yet. specd has no DCO requirement and no stated
  commit convention; adding a rewriting hook before the rule exists is
  enforcement without a policy. See [adoption 14](14-contributor-contract.md),
  which decides the policy first.
- Do not install hooks automatically from any other `make` target, from a test,
  or from `specd init`. Rewriting a contributor's git config without asking is
  the kind of implicit action this project refuses everywhere else.
- Do not add a hook that touches `.specd/`. Harness-owned state is never edited
  by tooling.

## Deferred

Parallel hook execution (buzz runs five formatters concurrently). specd has one
formatter; concurrency would be machinery around a single call.
