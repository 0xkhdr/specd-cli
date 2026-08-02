# Open work

Work that is known, named, and not done. This is not a roadmap and nothing here
is a commitment or a dated promise — the documentation in this repository
describes shipped behavior, and this file describes the gap between that and
what the project would like to be able to claim.

The boundary each release stands behind is
[`release/release-decision.md`](release/release-decision.md). Everything below
is either a limitation that record already names, or housekeeping that record
does not care about. When one of these is finished, the change belongs in the
release decision first and in this file second, and then it leaves this file:
what has been done is recorded there and in `CHANGELOG.md`, not here.

Written 2026-08-02, against `v0.3.0`.

## Re-proving the loop

These are the reasons the release decision says what a user gets is a working
base loop, fourteen replayed journeys, and an audited surface, and not more.

- [ ] **Drive a release itself through the loop.** What limitation 6 still says.
      Changing the release machinery went through the harness; cutting the tag,
      publishing the binaries, and writing the release decision did not.
- [ ] **Drive the loop end to end from two callers at once.** What limitation 2
      still says. Contention itself is proven —
      `TestConcurrentCallersOneRoot` races six real processes at one root, and a
      contested transition elects exactly one caller while racing appends lose
      nothing. What has never been run is two callers traversing the loop
      together rather than colliding on one operation.
- [ ] **Run a long-lived change.** Every traversal so far has been short. Not
      proven over a change that stays open across many commits.

## Platforms

- [ ] **Drive the loop end to end on macOS, Windows, or linux/arm64.** All three
      run the suite and replay the journeys, and all gate every release, but no
      change has been planned, approved, verified, and completed by hand on any
      of them. Until that happens they stay in the second tier: tests and
      journeys pass, the end-to-end guarantee is not claimed.
- [ ] **Two Windows guarantees are weaker than their Unix equivalents.** A
      managed write's directory entry is not flushed, because Windows exposes
      no call that flushes one; durability there rests on NTFS metadata
      journaling, and no fix is known. And the verification process tree is
      bound to its job object immediately after the process starts rather than
      before it runs, so a descendant spawned in that interval would escape
      termination; closing that one is reachable — start suspended, assign, then
      resume — and is deliberately not taken on faith, because a platform fact
      here is earned by a green run on that platform. Both are recorded in
      `release/release-decision.md`.

## Housekeeping

- [ ] **Drop the `v0.1.0` retraction when it stops mattering.** It is in
      `go.mod` because that tag ships a platform claim its own CI run disproved.
      Once no one could reasonably reach for it, the directive is noise.

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
