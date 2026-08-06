# Release decision

One compact record of what specd is, what has been proven about it, and what has
not. `internal/integration/release_test.go` parses this file: it requires every
section below, exactly one dated decision, and it forbids `release` while any
gate is red. This document projects retained truth and can never override it.

## Terms from the build

This record numbers things the way the process that produced specd numbered
them. None of it is part of the tool, and a reader who never saw that process
needs it defined once:

- **Stages 1–7** built the base loop: root and layout, state, planning
  artifacts, gates, approval, readiness and context, execution, evidence,
  completion, sync, and archive.
- **Stage 8** added the opt-in production profile — production reports, the
  separate reviewer verdict, policy, and friction records.
- **Stage 9** was release qualification: it fixed the fourteen journeys, the
  gate list, and the shape of this document, and it is the source of the rule
  that a platform claim is earned by a green run rather than inferred.
- **Stage 10** is the deferred work, listed under "Deferred domains and
  triggers". None of it was started.
- **D9** and **D14** are two numbered decisions from that process. D9 made
  harness locks mutual exclusion and never content. D14 set the threshold a
  deferred domain must clear before it may be planned at all.

## Implemented base loop

```text
init → new → author proposal/delta/design/tasks → check → human approve
→ next → context → start → edit declared files → verify → complete
→ human sync → sync → archive
```

Stages 1–7 are implemented: one selected root and managed layout, atomic
old-or-new state writes with revision guards, authored planning artifacts and
capability deltas, deterministic validation gates, content-hash human approval,
filesystem-derived readiness and the ready frontier, bounded task context,
declared-file scope enforcement, current-HEAD verification evidence,
harness-owned completion, one-transaction sync into accepted specs, and archive.
Stage 8 adds read-only production reports, a separate reviewer verdict, policy,
and friction records.

## Journey results

All fourteen stage 9 journeys are retained and replayed by
`internal/integration/release_journeys_test.go` over isolated fixtures, driving
the same CLI routes a caller drives. Evidence refs:

- runner and journey names: `internal/integration/release_journeys_test.go`
  (`TestReleaseJourneys`, subtests `01`–`14`);
- fixtures: `internal/integration/testdata/release/`;
- fresh-agent resume: `TestFreshAgentResume`;
- agent contract parity: `internal/integration/agent_contract_test.go`;
- surface ownership: `internal/integration/subtraction_test.go` and
  `release/surface-inventory.md`.

The refusal and recovery journeys (04, 05, 06, 07, 08, 09, 10, 11) each prove a
fail-closed refusal carrying exactly one legal next action, and each recovery
proves old-or-new durable bytes with no invented evidence, completion, approval,
sync, or archive.

## Release gates

Mechanically asserted by `TestReleaseQualification` from repository facts:

| gate | how |
| --- | --- |
| standard-library-only default binary | `go.mod` require set parsed; must be empty |
| formatting clean | every module `.go` file re-formatted in memory and byte-compared (`gofmt -l` without shelling out) |
| generated docs parity | `docs/operations.md` byte-compared with `core.RenderOperationDocs` |
| no broken link in the user documentation | every relative inline link in `README.md` and `docs/*.md` resolved against the filesystem |
| generated guidance parity | `generate.Render` is deterministic and names every agent-visible executable operation |
| all fourteen required journeys retained | required list is `requiredJourneys` in `release_test.go`, retained list parsed from the runner |
| no unowned surface | pending-deletion table of `release/surface-inventory.md` must be empty |
| no dead vocabulary in the user and agent surface | guidance template, generated operations document, and registry help scanned |
| no network or LLM path in the deterministic core | imports of `internal/core`, `internal/plan`, `internal/reconcile`, `internal/generate`, `internal/agentjson`, `internal/context` parsed |
| gate limits complete | every gate row in this section must have an exact heading in `release/gate-limits.md` |

Recorded as observed CI facts, not asserted here — running either from inside
this test recurses into it:

| gate | status |
| --- | --- |
| `go test ./... -race -count=1` | observed green on linux/amd64, linux/arm64, darwin/arm64, and windows/amd64, 2026-08-01 |
| `go vet ./...` | observed green on linux/amd64, linux/arm64, darwin/arm64, and windows/amd64, 2026-08-01 |
| bounded fuzz burst | path containment, ledger replay, and task parsing each explored for 60 seconds on a linux/amd64 developer host, 2026-08-06; the same burst now runs in the Ubuntu `repo` job on every push and pull request |
| `release/tag-contract.sh --self-check` | observed green in the `repo` job on ubuntu-latest, 2026-08-06, and on a linux/amd64 developer host the same day. Runs on every push and pull request now, not only inside the release workflow on a tag push |
| no advisory reachable from called code (`govulncheck`) | observed green in the `vulnerabilities` job on ubuntu-latest, 2026-08-06, and on a linux/amd64 developer host the same day. Runs on every push and pull request |

[Gate limits](gate-limits.md) records what each green row does not establish.

All five are run by `.github/workflows/ci.yml` on every push and pull request,
alongside the formatting and empty-require checks. That makes the observation
repeatable by anyone reading the run log instead of trusting the dates above.

What those last two gates do not catch. The tag-contract self-check asserts the
script's own accept/reject logic against a synthetic repository; it does not
assert that the release workflow calls the script correctly, and it runs only
on `ubuntu-latest`. `govulncheck` reports advisories reachable from called code
in the standard library and the toolchain — with an empty require set there is
nothing else for it to read — so it says nothing about a defect in specd's own
logic, and its verdict is only as current as the advisory database on the day
of the run. Neither is a substitute for the suite.

The CI matrix runs `ubuntu-latest`, `ubuntu-24.04-arm`, `macos-latest`, and
`windows-latest`. `windows-latest` was run on 2026-08-01, failed 254 tests, was
removed, and is back only because the port that made it pass was written and
observed green the same day; `ubuntu-24.04-arm` is new and green on its first
run. A platform claim in this document is earned by a green run, not by the
presence of a runner, and the tiers below are stated separately so neither is
inferred from the other.

Red gates right now: none.

Two gates that ran on 2026-07-31 no longer run. `stages 1-7 proven end to end by
the dogfood change` read the dogfood change's harness-owned `state.json`, and the
amendment-completeness check read the collected build amendments; the publishing
cleanup on 2026-08-01 removed `.specd/` and `build/` from the tree, so neither
has an input. Their subject matter was not disproven — it stopped being
mechanically re-checkable in a checkout. The journey list those gates shared a
source with is now `requiredJourneys` in `release_test.go`, which still fails on a
dropped, renamed, or added journey.

The dogfood change traversed the whole loop in this root on 2026-07-31 and was
archived at `.specd/archive/2026-07-31-release-journey-runner/` at lifecycle
`archived`, revision 8, both tasks `completed`. Both human gates were passed by
a human on a terminal and by no one else: `approve` (revision 1→2, aggregate
`010e98c6`) and `sync` (revision 6→7). The agent-driven steps refused at each
gate and were left refused. No approval was faked, and no `.specd` state,
history, or evidence byte was hand-edited at any point.

That traversal is not verifiable by anyone but its operator. The `.specd/` root
holding it was removed from the tree in the 2026-08-01 publishing cleanup, and
the repository published at `github.com/0xkhdr/specd-cli` begins at a single
initial commit that does not contain it — so the Git history a reader can clone
does not carry it either, and neither does any surviving working copy. What
remains is this paragraph: an operator's account, uncorroborated by an artifact
a third party can inspect. Weigh it as testimony. Every mechanically re-checkable
claim in this document is elsewhere — in the gate table above, in the fourteen
replayed journeys, and in the tests — and none of them depends on this one.

That gap is what the 2026-08-01 traversal closes, and it closes it with
artifacts rather than prose. `.specd/evidence.jsonl` holds three verification
records (`2d6b55e3`, `3684e633`, `e6482133`), one per task of
`docs-navigation`, each non-vacuous and passing at the HEAD it was observed at.
`.specd/history.jsonl` carries the whole traversal in order — created,
approved, three attempt/completion pairs, synced, archived — with the two human
gates recorded against a human identity and every other step against the
implementing actor. `.specd/specs/documentation/spec.md` is accepted truth
reconciled by `sync` rather than authored by hand, and
`.specd/archive/2026-08-01-docs-navigation/` is where the change came to rest.
All of it is in the published Git history, so a reader re-derives it from a
checkout instead of weighing testimony.

## Assurance boundary

- The harness owns lifecycle, validation, approval freshness, declared scope,
  evidence applicability, completion, atomic writes, sync, and archive. It
  enforces these over its own managed bytes.
- The harness declares scope; it does not isolate a process. Only a conformant
  host can contain one. Host assurance is advisory unless the host proves
  stronger containment.
- The human route is derived from a termios ioctl, so only a real controlling
  terminal derives it and every other stdin derives agent. `SPECD_ROUTE` is
  still a host declaration and still provenance rather than proof: the harness
  can refuse an agent that did not declare itself, never attest a human.
- Verification evidence is an observation pinned to current HEAD. It never
  completes a task by itself, and completion never runs a verification.
- Reports and this document project truth. They authorize nothing.
- Supported platform claims are limited to the platform actually run.
- The real-root traversal of 2026-07-31 is uncorroborated. It is neither in the
  published tree nor in the published Git history, so the harness cannot
  re-derive it and a reader cannot check it. Treat it as operator testimony, not
  as evidence. The traversal of 2026-08-01 does not corroborate it either — it
  is a second, independent traversal, and it is the one a reader can check.

## Known limitations

1. **D14 eligibility has a route but no records in this root.** `specd friction`
   is registered, dispatched, and replayed by journey 03, so the stage 9
   requirement to append friction at observation time now has a route. No
   friction record exists in this root: the three observations below predate the
   operation and were not back-dated into history. The D14 threshold is
   therefore still unmet and every deferred domain stays blocked. The build
   amendments those observations were recorded in are not in the published
   repository; the three of them are restated under "Recorded frictions" below,
   which is now their only surviving record.
2. **Three traversals are three traversals.** The base loop is proven end to end
   by a two-task change on 2026-07-31 and three-task changes on 2026-08-01 and
   2026-08-02, all in one root on one platform. It is not proven at scale or
   over long-lived changes. Contention is narrower than it was:
   `TestConcurrentCallersOneRoot` races six real `specd` processes against one
   root and asserts that a contested transition elects exactly one caller while
   every loser fails closed on a named refusal with one legal next action, and
   that independent appends to the shared history ledger all survive a clean
   replay. Processes rather than goroutines, because the in-process mutex in
   `internal/core/lock` would satisfy an in-process race without the file lock
   ever being exercised. What that does not establish is the loop driven end to
   end from two callers at once, which still has not been done.
3. **Registration is not visibility.** `report` and `review` were registered,
   documented, and unreachable until the envelope projection was fixed. The
   class of defect is now covered by reachability tests, not by assumption.
4. **Scope enforcement counts git-ignored files, and that surprised a real
   run.** `verify` refused this change's first attempt because two ignored
   artifacts sat in the tree. That is deliberate — honoring `.gitignore` would
   let an agent write anywhere by adding an ignore rule — but it means a dirty
   working tree blocks the loop for reasons no plan mentions.
5. **The re-proof is one change, in one root, on one platform, in one day.**
   `docs-navigation` traversed the whole loop in this root on 2026-08-01:
   created at 11:13, approved at 16:51 by a human on a terminal, three tasks
   each started, verified against real evidence, and completed, synced at 18:21
   by a human, and archived at
   `.specd/archive/2026-08-01-docs-navigation/`. Unlike the 2026-07-31
   traversal, every step of it is in the published Git history and in
   `.specd/history.jsonl`, so a reader can re-derive it from a checkout instead
   of trusting its operator. What it does not establish is duration or
   contention: it ran on linux/amd64, in one root, across a few hours, with no
   second caller.
6. **The release machinery has been driven through the loop once; the release
   itself still has not.** `release-contract-gate` traversed the whole loop in
   this root on 2026-08-02 and changed the release workflow: three tasks, each
   started, verified against real evidence, and completed, with the plan and
   every task in the published Git history. That answers the part of this
   limitation that said the machinery had never gone through its own harness.
   What it does not answer is the release: cutting a tag, publishing binaries,
   and writing this record are still ordinary edits, not a change under
   `.specd/`, and the loop has never driven one.
7. **An attempt is bound to one commit and only `complete` releases it.**
   `release-contract-gate` was planned three times before it ran. `start` binds
   an attempt to the commit HEAD was at, and the scope check counts uncommitted
   planning artifacts as paths outside that attempt's authority — both correct
   on their own. Together they mean a plan that is still uncommitted when its
   first task starts cannot be committed afterwards: committing moves HEAD,
   `verify` then refuses on a moved baseline and directs the caller to
   regenerate context, `context` refuses because an in-progress task is not in
   the frontier, and `start` refuses because an attempt is already bound. No
   operation rebinds or discards an attempt, so the only exit is to rebuild the
   change and spend a second human approval. The refusal names a next action the
   tool cannot perform, which is the one thing a fail-closed refusal must not
   do.
8. **A declared file list separated by commas is accepted at plan time.** The
   `files` column splits on `;`. Commas parse as a single path whose name
   contains commas, which `check` accepts, and which surfaces only when that
   task starts and its scope refuses every real file it needs. Combined with
   limitation 7, the defect cost a full rebuild of the change. The plan gate
   sees the declared paths and could refuse one that cannot exist.

## Recorded frictions

Observed during stage 9, before `specd friction` existed, so recorded here
rather than as `friction` records. They are left as
prose deliberately: a record appended now would claim an observation date and a
state revision that were never observed.

| observation | change / task | missing capability |
| --- | --- | --- |
| `specd report` and `specd review` returned no result through the CLI | S8-RPT-02 | envelope projection for new result types |
| report JSON keys were rejected by the envelope key gate | S8-RPT-02 | snake_case emission in the report model |
| no route exists to record friction | S9-REL-01 | a friction operation (resolved: `specd friction`) |

These are evidence, not authorization. Two of them name the same missing
capability class but arise from one change, so no D14 threshold is met and no
deferred domain is authorized.

## Deleted surface

Raised by `TestSurfaceOwnership` as exported-and-unreferenced, deleted by
separately scoped tasks:

| symbol | task |
| --- | --- |
| `internal/core/path.(*Owner).RecordLock` | S9-SUB-02 |
| `internal/core/state.ErrStaleRevision` | S9-SUB-03 |
| `internal/core/transaction.RecoverUnderRootLock` | S9-SUB-04 |

None carried validation, approval, authority, scope, evidence, staleness,
atomicity, or fail-closed behavior; none had a caller or a test. No compatibility
shim or deprecation remains, because D11 defines no installed compatibility base.
`release/surface-inventory.md` now maps every survivor to one owner and its
pending-deletion table is empty.

## Deferred domains and triggers

No stage 10. Orchestration, delivery, maintenance, multi-root views, migration
and importers, network services, LLM evaluators, telemetry, plugins, and a
second adapter are all deferred. Each enters only as an ordinary change after
D14: two friction records from two independent changes or tasks naming the same
missing capability, plus a dated root-owner authorization. The threshold is
currently unreachable (limitation 1), so every deferred domain stays blocked.

## Supported platforms

Two tiers, deliberately not merged. Both are observations rather than
inferences: the platforms below were run on 2026-08-01, and what they did is
recorded whether or not it flattered the project.

**Supported.** linux/amd64. All journeys, tests, vet, and formatting were run
there, by hand and by CI, and the one real-root traversal was driven there.

**Journeys replay; no change has been driven through the loop by hand.**
linux/arm64 (`ubuntu-24.04-arm`), darwin/arm64 (`macos-latest`), and
windows/amd64 (`windows-latest`). On each of these the full `-race` suite is
green and all fourteen release journeys replay, and every one of them gates
every release, so no artifact ships unless it passed on all four. That is the
whole claim. Nobody has planned, approved, verified, and completed a real
change on any of them, so the end-to-end guarantee stays limited to
linux/amd64.

Windows reached that tier on 2026-08-01 and not before. The same day's first
run failed 254 tests across 14 packages, and the causes were structural: every
managed write ended in a directory flush Windows does not implement, the
checkout arrived as CRLF and drifted every golden fixture, the acting identity
was read from `USER` alone, the change lock lived inside the folder `archive`
renames, and process-tree termination went through `taskkill`, whose exit
status cannot distinguish a descendant it failed to kill from one that had
already exited. Two Windows facts survive the port and are weaker than their
Unix equivalents, so they are named rather than smoothed over:

- A managed write's directory entry is not flushed, because Windows exposes no
  call that flushes one. Durability of the rename rests on NTFS metadata
  journaling. The file's own bytes are still fsync'd before the rename.
- Binding the verification process tree to its job object happens immediately
  after the process starts rather than before it runs. A descendant spawned in
  that interval would escape termination. The interval precedes the shell
  reading its command line, and closing it needs a thread handle the standard
  library does not expose.

The macOS result was not free either. The first run failed 19 tests across 5 packages:
every one was a test comparing against a raw `t.TempDir()` while production
canonicalizes the selected root through `filepath.EvalSymlinks`, which diverges
wherever the temporary directory sits under a symlink — `/var/folders` on
macOS. The harness was correct and the tests were wrong. This is the class of
defect the previous decision predicted when it said one traversal would not be
the last to find one, and it was found only because a second platform was
actually run.

Stage 9 forbids inferring a platform claim from an unrun one. Nothing here is
inferred. When a real change is driven through the loop on one of the second
tier's platforms, move that platform up and say so with a date.

## Stage 8 status

Experimental. Production reports, the reviewer verdict, policy, and friction
records are additions above the default profile and are labelled as such. They
weaken no default behavior: the default profile's gates, approval, scope,
evidence, and completion rules are unchanged by their presence, and the
production profile is opt-in through `report --profile` and `review`. No
production-profile assurance is claimed.

## Decision

The base loop, the fourteen journeys, the subtraction audit, and every
mechanical gate that still has an input are green, on four runners rather than
two. The prior decision's own next steps have been taken in part: a real change
was driven through this loop and archived here, and two platforms moved from
unclaimed to observed.

A green board permits `release`. It does not establish maturity, and nothing
below is withdrawn by deciding to publish.

What changed since the 2026-08-01 `v0.1.1` decision is evidence, not scope. The
tool gained no capability. It gained a checkable traversal — `docs-navigation`,
planned, approved by a human, executed as three tasks against three
verification records, synced, and archived, with every step in the published
Git history rather than in an operator's account. It gained Windows and
linux/arm64, each earned by a green run after a port that cost five distinct
defects, and lost the claim that Windows is unsupported. The 2026-07-31
traversal stays uncorroborated; it is simply no longer the only one.

The publication path `github.com/0xkhdr/specd-cli` is unchanged, and so is the
reason for it: the previous module path has six versions permanently cached by
the module proxy and notarized in the checksum database, pointing at an
abandoned lineage this repository no longer contains.

So this decision rests on more than the one it replaces, and on the same kind
of thing. It is not a claim that the loop is load-bearing; it is the root owner
accepting that the loop is publishable at its stated boundary and that the
boundary is written down here honestly. What a user gets is: a working base
loop, fourteen journeys replayed on every platform a binary ships for, an
audited surface, a suite that passes on three operating systems and two
architectures, and one traversal they can re-derive from a checkout. What a
user does not get is proof at scale, under concurrent callers, over a
long-lived change, or that the loop has been driven through a real change
anywhere but linux/amd64. Anyone relying on this beyond that boundary is
relying on something no gate in this repository asserts.

Decision (2026-08-01, root owner): release

This publication is young, and its `0.x` version number says so: it asserts no
stability beyond the boundary above, and a minor bump may break anything. This
release changes one on-disk path — a change's lock moved out of the folder
`archive` renames — which no root reads and no migration needs, but which an
older root's `.specd/.gitignore` does not cover. Next: drive a real change
through the loop on macOS, Windows, or linux/arm64 rather than inferring the
platform from a green suite; put this repository's own release machinery
through the loop, which is the part that never has been; exercise two
concurrent callers against one root; and run a change that stays open across
many commits. Record friction through `specd friction` when a deferred domain
blocks real work — D14 has a route, this root has zero records, so the
threshold remains honestly unmet and every deferred domain stays blocked.
Revisit this record on the first of those results, and re-decide rather than
amend.
