# Contributing

The practical build, test, and extension guide is
[`docs/contributing.md`](docs/contributing.md).

[`AGENTS.md`](AGENTS.md) is the binding contributor and agent guide. Read it
before changing anything — it defines the vocabulary, the design rules, and what
the release gates refuse.

## Before you open a pull request

The checks, the release gates, and the regeneration commands are listed once, in
[`docs/contributing.md`](docs/contributing.md#before-you-hand-off). Run them
there rather than from a second copy here.

Search open issues and pull requests first. Link the closest result in the pull
request template, or write `none found`. For anything beyond a small fix, open
an issue before writing the change: maintainer review is the enforcement point,
and early agreement avoids work the repository cannot safely accept.

## Commits and pull requests

One logical change per pull request. Explain what behavior changed and how you
verified it — a reviewer should not have to guess which command you ran.

This repository retains ordinary Git history and has no commit-message or DCO
gate. Use a short imperative subject, but do not add sign-off trailers to satisfy
a policy that does not exist. Pull requests merge only after the required CI
checks and maintainer review pass; those checks, not a subject-line convention,
are the enforced contract.

If a change alters what the project claims to have proven — a platform, a
guarantee, a boundary — say so in
[`release/release-decision.md`](release/release-decision.md) in the same pull
request. That file is the boundary this project stands behind, and it is meant
to be edited honestly rather than kept flattering.

## What will not be merged

These are refusals, not style preferences:

- a runtime dependency (`go.mod`'s empty require set is a release gate);
- exported surface without an owner (`TestSurfaceOwnership` fails);
- a second output surface or a command guard that repeats a core refusal
  (`TestSurfaceOwnership` and the one-owner review rule apply);
- a bypass for evidence, approval, authority, scope, or validation (the bite
  tests and retained journeys must remain green);
- a weakened bite test or unrelated golden refresh used to hide a failure
  (review compares the asserted forbidden state with the changed behavior);
- speculative abstraction or unrelated drive-by churn (maintainer review
  refuses scope that no approved requirement owns).

Large refactors and new behavior need prior issue agreement because their
review boundary cannot be reconstructed safely after the code arrives.

## Agent-authored contributions

Agent-authored contributions are welcome and need no tool disclosure. You are
the human gate for your pull request: read the final change, understand its
claims, and own its effects. Maintainer review may close an evidently unreviewed
submission and point back here; the harness makes work reviewable, not approved.

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
unpublish it; it only makes the tag and the proxy disagree.

A release that is merely improved on is superseded: the next release says so in
its `CHANGELOG.md` entry and nothing else happens. A release that should not be
selected at all is retracted with a `retract` directive in `go.mod`, which is
the only supported way to take a version back — the tag stays, the version stays
resolvable, and `go get` stops choosing it. `v0.1.0` is retracted, and the
directive above it says why: it ships a platform claim its own release run had
already disproved. Retraction is for a version that misleads or breaks, not for
one that is simply old.

## Reporting security issues

Do not open a public issue. See [`SECURITY.md`](SECURITY.md).
