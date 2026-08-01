# Open work

Work that is known, named, and not done. This is not a roadmap and nothing here
is a commitment or a dated promise — the documentation in this repository
describes shipped behavior, and this file describes the gap between that and
what the project would like to be able to claim.

The boundary each release stands behind is
[`release/release-decision.md`](release/release-decision.md). Everything below
is either a limitation that record already names, or housekeeping that record
does not care about. When one of these is finished, the change belongs in the
release decision first and in this file second.

Written 2026-08-01, against `v0.2.0`.

## Re-proving the loop

These are the reasons the release decision says what a user gets is "a working
base loop, fourteen replayed journeys, an audited surface" and not more.

- [x] **Drive one real change through specd's own loop.** Done 2026-08-01.
      `docs-navigation` went created (11:13) → approved by a human (16:51) →
      three tasks each started, verified, and completed → synced (18:21) →
      archived, and every step is in the published Git history.
- [x] **Do it with a change that is not release machinery.** Done. It was
      documentation, not workflows or platform claims.
- [x] **Put the release machinery itself through the loop.** Done 2026-08-02.
      `release-contract-gate` changed `.github/workflows/release.yml` through
      three tasks, each started, verified, and completed, and it cost two
      abandoned plans and three human approvals to get there — the reasons are
      limitations 7 and 8 of the release decision, both found by doing this.
- [ ] **Drive a release itself through the loop.** What limitation 6 still says.
      Changing the release machinery went through the harness; cutting the tag,
      publishing the binaries, and writing the release decision did not.
- [ ] **Exercise two concurrent callers against one root.** The root and change
      locks serialize managed writes, but nothing has proven it. The release
      decision claims no proof under concurrent callers, and `SECURITY.md` says
      a concurrency defect is a real bug but not a surprise.
- [ ] **Run a long-lived change.** Every traversal so far has been short. Not
      proven over a change that stays open across many commits.

## Corroboration

- [x] **The 2026-07-31 real-root traversal is operator testimony.** Answered
      2026-08-01, not by corroborating it but by replacing its role: the
      `docs-navigation` traversal is a second, independent one, and it lands in
      this repository's history where a reader can check it. The 2026-07-31
      account stays testimony and is still labelled as such.

## Platforms

- [x] **Windows is a port, not a bug list.** Done 2026-08-01. The suite and all
      fourteen journeys are green on `windows-latest`. The real causes were not
      the ones guessed here: the directory flush every managed write ended in
      does not exist on Windows, the checkout arrived as CRLF and drifted every
      golden fixture, the actor came from `USER` alone, the change lock lived
      inside the folder `archive` renames, and `taskkill` cannot report whether
      a tree died. The human-route question is answered by `GetConsoleMode`,
      which is the same kind of fact the termios probe reads.
- [x] **linux/arm64 is unobserved.** Done 2026-08-01. `ubuntu-24.04-arm` is in
      the matrix and green on its first run, and a binary is published for it.
- [ ] **Drive the loop end to end on macOS, Windows, or linux/arm64.** All three
      run the suite and replay the journeys, and all gate every release, but no
      change has been planned, approved, verified, and completed by hand on any
      of them. Until that happens they stay in the second tier: tests and
      journeys pass, the end-to-end guarantee is not claimed.
- [ ] **Two Windows guarantees are weaker than their Unix equivalents.** A
      managed write's directory entry is not flushed, because Windows exposes
      no call that flushes one; durability there rests on NTFS metadata
      journaling. And the verification process tree is bound to its job object
      immediately after the process starts rather than before it runs, so a
      descendant spawned in that interval would escape termination. Both are
      recorded in `release/release-decision.md`.
- [x] **README and `docs/` said Windows is unsupported.** Corrected for
      `v0.2.0`, after `docs-navigation` archived and the files were free.

## Housekeeping

- [x] **Decide what happens to `github.com/0xkhdr/specd`.** Done 2026-08-01.
      Its description now names `github.com/0xkhdr/specd-cli` as the live path,
      and the repository is archived. The six proxied versions are still
      immutable and still point at the abandoned lineage; nothing can fix that,
      so the fix was to make the pointer obvious.
- [x] **Set the repository description and topics.** Done 2026-08-01.
- [x] **Consider branch protection on `main`.** Done 2026-08-01. `repo` and
      all four `gates` legs are required and must be up to date; force pushes
      and deletion are blocked. Admins are exempt, so the rule binds everyone
      else and the release workflow still re-runs the gates because a tag can
      point at any commit.
- [ ] **Drop the `v0.1.0` retraction when it stops mattering.** It is in
      `go.mod` because that tag ships a platform claim its own CI run disproved.
      Once no one could reasonably reach for it, the directive is noise.
- [x] **Stale internal naming in `release/release-decision.md`.** Done for
      `v0.2.0`: "specd v2" is gone from the opening, and a "Terms from the
      build" section defines stages 1–10, D9, and D14 once, so the references
      resolve for a reader who never saw that process.

## Deferred domains

Not work — a standing refusal, recorded here so it is not mistaken for an
oversight. Orchestration, delivery, maintenance, multi-root views, migration
and importers, network services, LLM evaluators, telemetry, plugins, and a
second adapter are all deferred. Each enters only as an ordinary change after
D14: two friction records from two independent changes or tasks naming the same
missing capability, plus a dated root-owner authorization.

This root has zero friction records, so the threshold is honestly unmet and
every deferred domain stays blocked. Record friction through `specd friction`
when a deferred domain blocks real work — that is the only route in, and
inventing one is not.
