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

Written 2026-08-01, against `v0.1.1`.

## Re-proving the loop

These are the reasons the release decision says what a user gets is "a working
base loop, fourteen replayed journeys, an audited surface" and not more.

- [~] **Drive one real change through specd's own loop.** In flight as of
      2026-08-01: `docs-navigation` is approved, task D1 is completed against
      real evidence, and D2 is open. Not finished until the change is synced
      and archived. This is limitation 5.
- [ ] **Do it with a change that is not release machinery.** The v0.1.0/v0.1.1
      work — module path, workflows, platform claims — was done as ordinary
      edits rather than as a change under `.specd/`. That is limitation 6, and
      it is exactly what limitation 5 says the next change should not be.
- [~] **Finish `docs-navigation`.** No longer stranded: it is approved and D1 is
      complete. Two tasks remain, and the platform tiers changed underneath it,
      so its "Honest production boundary" requirement now has a different truth
      to state than it did when the plan was approved.
- [ ] **Exercise two concurrent callers against one root.** The root and change
      locks serialize managed writes, but nothing has proven it. The release
      decision claims no proof under concurrent callers, and `SECURITY.md` says
      a concurrency defect is a real bug but not a surprise.
- [ ] **Run a long-lived change.** Every traversal so far has been short. Not
      proven over a change that stays open across many commits.

## Corroboration

- [ ] **The 2026-07-31 real-root traversal is operator testimony.** It is not in
      the published tree and not in the published Git history, which begins at a
      single initial commit. Nobody but its operator can check it. It is
      recorded honestly as testimony rather than evidence, and the only fix is a
      new traversal that lands in this repository's history.

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
- [ ] **README and `docs/` still say Windows is unsupported.** They were true
      when written and are not now. They are the declared scope of the
      in-flight `docs-navigation` change, so the correction belongs in that
      change rather than beside it.

## Housekeeping

- [ ] **Decide what happens to `github.com/0xkhdr/specd`.** The old repository
      is empty, public, and not archived. Its module path is permanently burnt:
      six versions are cached by the module proxy and notarized in the checksum
      database, pointing at an abandoned lineage. Options are to archive it, or
      to publish a final version whose `go.mod` marks the module deprecated and
      points at this path. Doing nothing leaves a repository that resolves to
      abandoned code for anyone who finds it first.
- [ ] **Set the repository description and topics.** Both are empty on
      `0xkhdr/specd-cli`. This is the only thing a search result shows.
- [ ] **Consider branch protection on `main`.** CI is advisory right now:
      nothing prevents a push that skips it, and the release workflow only
      re-runs the gates because a tag can point at any commit. Requiring the
      `repo` and `gates` checks would make the CI observation load-bearing
      rather than customary.
- [ ] **Drop the `v0.1.0` retraction when it stops mattering.** It is in
      `go.mod` because that tag ships a platform claim its own CI run disproved.
      Once no one could reasonably reach for it, the directive is noise.
- [ ] **Stale internal naming in `release/release-decision.md`.** It opens with
      "what specd v2 is" and refers throughout to "stage 9" and "stage 10" —
      vocabulary from the build process that produced the tool, not from the
      tool. A reader outside that process cannot resolve it. Either define the
      stages once or rewrite the references.

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
