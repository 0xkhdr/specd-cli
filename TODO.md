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

- [ ] **Drive one real change through specd's own loop.** `.specd/` is live and
      holds `docs-navigation` at stage `planning`, revision 1 — zero approvals,
      zero tasks started, zero evidence. The loop is available here and has not
      been run here since the tree was published. This is limitation 5.
- [ ] **Do it with a change that is not release machinery.** The v0.1.0/v0.1.1
      work — module path, workflows, platform claims — was done as ordinary
      edits rather than as a change under `.specd/`. That is limitation 6, and
      it is exactly what limitation 5 says the next change should not be.
- [ ] **Finish or drop `docs-navigation`.** It is stranded at `planning`. Its
      described outcome — README as a landing page, `docs/README.md` as the
      documentation map — is already applied to the tree by hand, so either
      drive it through the loop as the first real change or retire it. Leaving
      a planning-stage change that describes already-shipped work is the kind
      of stale state the harness exists to prevent.
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

- [ ] **Windows is a port, not a bug list.** Run on 2026-08-01 and failed 254
      tests across 14 packages. Two structural causes: CRLF line endings in
      artifact parsing and digesting, and `\` path separators in scope,
      selector, and manifest handling. There is also a third assumption to
      settle — the human route is derived from a termios ioctl, which has no
      direct Windows equivalent, so "how does a Windows console prove a human"
      is a design question before it is a code question. Do not re-add
      `windows-latest` to the matrix until that is answered; a red runner
      claims nothing.
- [ ] **Drive the loop end to end on macOS.** The `-race` suite is green there
      and gates every release, but no change has been planned, approved,
      verified, and completed on macOS. Until that happens macOS stays in the
      middle tier: tests pass, the end-to-end guarantee is not claimed.
- [ ] **linux/arm64 is unobserved.** No runner, no binary, no claim. If a binary
      is ever published for it, the architecture needs a real test run first
      rather than a cross-compile and an assumption.

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
