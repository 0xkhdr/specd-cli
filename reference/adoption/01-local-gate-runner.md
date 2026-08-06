# 01 — One command reproduces CI

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P1](../patterns.md#p1--one-command-reproduces-ci) | 1 | small | low | applied 2026-08-06 |

## Why

buzz's entire local quality interface is `just ci`, and `CONTRIBUTING.md` binds
it: "This is the same check that runs in CI. PRs that fail `just ci` will not be
merged."

specd's gate is smaller but is currently assembled from memory across three
files: `AGENTS.md` gives `go test ./... -race -count=1 && go vet ./...` plus the
docs-regeneration command, `README.md` gives the two-command handoff check, and
`.github/workflows/ci.yml` additionally runs `gofmt -l .`, the empty-require-set
check, and `--version`/`--help` smoke. A contributor who runs the documented
handoff check can still fail CI on formatting. That is exactly the failure mode
buzz calls out with "Clippy passing does not mean fmt passes; run both."

## What specd already has

- `.github/workflows/ci.yml` — the authoritative gate list.
- `release/tag-contract.sh` — an existing precedent for a checked-in POSIX `sh`
  script in this repository.

## Do not use `just`

`just` is a third-party binary. specd ships one static binary with zero
dependencies and treats an added dependency as a release-gate failure; asking a
contributor to install a task runner to run three Go commands inverts that
value. Use `make`, which is present on every developer host specd targets and is
not vendored, imported, or shipped.

## Change set

Create `Makefile` at the repository root:

```make
# The local projection of .github/workflows/ci.yml. CI remains the authority:
# it calls the underlying commands directly so a reader of the workflow never
# has to resolve a Makefile, and so the Windows leg needs no make at all.
# If a gate is added to the workflow, it is added here in the same commit.

.PHONY: ci check test vet fmt fmt-check deps-empty smoke docs hooks

ci: check test smoke

check: fmt-check vet deps-empty

test:
	go test ./... -race -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	  test -z "$$unformatted" || { echo "$$unformatted"; exit 1; }

deps-empty:
	@test -z "$$(go list -m all | tail -n +2)" \
	  || { echo "specd is standard library only; a dependency was added"; exit 1; }

smoke:
	go run ./cmd/specd --version
	go run ./cmd/specd --help

# Regenerate the operations document after an operation-registry change.
docs:
	SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity

# Install the repository's git hooks (see reference/adoption/02).
hooks:
	git config core.hooksPath .githooks
```

Then update the three places that currently state the check:

- `AGENTS.md` §Checks — lead with `make ci`, keep the raw commands beneath it
  for the platforms and shells where `make` is absent, and state the binding
  sentence: *`make ci` is the local projection of `.github/workflows/ci.yml`;
  if a gate is added to one, it is added to the other in the same commit.*
- `README.md` §Contributing — replace the two-command block with `make ci`.
- `docs/contributing.md` — same, plus `make docs` for regeneration.

## Keep the workflow honest

Do **not** rewrite `.github/workflows/ci.yml` to call `make`. Two reasons, both
of which buzz demonstrates the cost of:

1. The workflow's steps are individually named in CI output; collapsing them
   into `make ci` makes a failure report "make ci failed" instead of "gofmt".
2. The `windows-latest` leg has no guaranteed `make`. buzz solved a comparable
   problem with Hermit; specd solves it by not creating it.

The parity between the two is a documented obligation, not a shared
implementation. Add a comment in the workflow saying so.

## Acceptance

```bash
make ci                       # green on a clean checkout
gofmt -w internal/ && git diff --quiet   # no drift introduced
```

Deliberately break each gate one at a time (add a stray space, `import "fmt"`
without use, add a `require` line to `go.mod`) and confirm `make ci` fails on
that gate and names it. A `Makefile` that cannot fail is a `Makefile` nobody
needs.

## Applied notes (2026-08-06)

Two deviations from the change set above, both deliberate:

- `deps-empty` checks `go list -m all`'s exit status before its output. Running
  the acceptance breakage found that the version in this doc — and the identical
  one already in `.github/workflows/ci.yml` — passed when `go list` *failed*: a
  failing `go list` prints nothing, and nothing satisfied `test -z`. A `go.mod`
  with a require line and no `go.sum` entry, which is exactly the state a newly
  added dependency is in, was accepted. The workflow was fixed in the same
  commit, so the projection still matches.
- `make check` includes `vuln` (adoption 04), which needs network.
  `make check-offline` is the same list without it, so a host with no network
  still has a runnable gate.

Every gate was verified to bite: a stray space fails `fmt-check` and names the
file, an unused-format-verb call fails `vet`, and a require line fails
`deps-empty` both with and without a resolvable `go.sum`.

## Do not

- Do not add `just`, `task`, `mage`, or any other runner.
- Do not put logic in the `Makefile` that does not exist in CI. It is a
  projection, not a second source of truth — the same rule `AGENTS.md` applies
  to `docs/operations.md`.
- Do not make hooks install automatically from `make ci`. Installing a hook is a
  separate, consented action (`make hooks`).

## Deferred

No `make setup`, `make reset`, or environment bootstrap: specd needs a Go
toolchain and nothing else. Add one only if a future adoption introduces
external state a contributor must create.
