# 12 — ARCHITECTURE.md

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P11](../patterns.md#p11--documentation-is-organized-by-failure-mode) | 4 | medium | low | applied 2026-08-06 |

## Why

buzz's `ARCHITECTURE.md` is 827 lines in nine sections: Executive Summary, The
Protocol, Connection Lifecycle, Event Pipeline, Subscription System, Crate
Reference, Security Model, Infrastructure, **Known Limitations**. It is the
document `CONTRIBUTING.md` and `AGENTS.md` both point at for "how the system is
shaped."

specd has excellent documentation of *what it does* (`docs/concepts.md`,
`docs/the-loop.md`, `docs/layout.md`) and of *what has been proven*
(`release/`). It has no document explaining **how the code is shaped**. Today a
contributor or agent must reconstruct that from `release/surface-inventory.md`,
which is a data table sorted by ownership, not an explanation — and `AGENTS.md`
explicitly calls it "data, never a second source of truth."

The concrete cost: the design rules in `AGENTS.md` ("one canonical parser per
contract", "one output owner", "one validation boundary") are stated as rules
without the layer diagram that makes them obvious. A reader who cannot see the
layering re-derives it wrong and adds the second surface the rule forbids —
which has already happened at least once, judging by the commit "Delete the
second output surface and the guards that shadow core."

## Scope

Not 827 lines. specd is one binary; target 200–300, and make the last section
the one that pays for the document.

Proposed outline for `ARCHITECTURE.md` at the repository root (buzz keeps it at
root, and `AGENTS.md` already links root-level `release/` files):

```markdown
# specd architecture

## 1. Summary
One process, no daemon, no network. A CLI parses one invocation, dispatch
routes it to one core operation, core reads and writes a managed root under a
lock, and one envelope is projected to text or JSON. Everything else is a
detail of those five steps.

## 2. Layers
argv → internal/cli → internal/cmd (dispatch + the one output owner)
     → internal/core (the only place a decision is made)
     → internal/core/{state,record,persist,transaction} (durability)
     → filesystem + git

State the direction rule: dependencies point inward, and `core` imports nothing
from `cmd`. Say what enforces it.

## 3. The one-owner rules and what enforces them
- one output owner — cmd.RenderJSON / cmd.RenderText, gated by
  internal/integration/subtraction_test.go
- one validation boundary — core refuses; cmd does not restate
- one canonical parser per contract — resolution, task parsing, command
  metadata, JSON envelopes
Each with the test that fails when it is violated.

## 4. The operation registry
The central mechanism: one registry projects `--help`, `docs/operations.md`,
and the managed block of `AGENTS.md`. What a registry entry contains; what
regenerating costs; the parity gate. This is the section a contributor reads
before adding an operation.

## 5. Durability
Atomic old-or-new writes, revision guards, the lock model (D9: mutual
exclusion, never content), append-and-replay ledgers, what happens on an
interrupted append.

## 6. The trust model in code
Where the human/agent route is derived (the termios ioctl on stdin) and why it
cannot be reached from an agent-routed invocation. Where declared scope is
validated. Where evidence applicability is decided. Cross-link SECURITY.md
rather than restating it.

## 7. Package map
One line per package under internal/, naming its single responsibility.
Cross-link release/surface-inventory.md for ownership, and say explicitly that
this section explains shape while that file records ownership — two documents,
two jobs, no overlap.

## 8. Known limitations
Host assurance is advisory. No containment. Scale unmeasured beyond
release/scale.md. Concurrent end-to-end operation by two callers is not
claimed. Windows and macOS are gated but not hand-driven.
Cross-link release/gate-limits.md.

## 9. Design decisions and what would reverse them
Why standard-library-only. Why Markdown and Git as the user-visible truth. Why
one binary. Why no daemon. For each: the condition under which it would be
revisited.
```

Section 9 is the one buzz does not have and specd should, because it is the
project's actual character — `release/release-decision.md` already records
deferred domains and their triggers, and this is the same discipline applied to
architecture rather than to features.

## Change set

- Create `ARCHITECTURE.md`.
- Link it from `AGENTS.md` §Read first (as item 4, before "source and tests"),
  `docs/README.md` §Contribute, and `docs/contributing.md`.
- Add it to the documentation ownership table in `docs/README.md`:
  `| Code shape and layering | ARCHITECTURE.md |`.
- Confirm the broken-relative-link release gate covers it. If that gate scans
  only `README.md` and `docs/*.md`, extend it to `ARCHITECTURE.md` in the same
  commit — a new document outside the gate is a new place for links to rot.

## Acceptance

- `go test ./... -count=1` green, including the link gate over the new file.
- The document names the enforcing test for every rule it states. A rule without
  its enforcement named is the thing P12 forbids.
- Sanity check: a reader who has never seen the repository can, from sections 2
  and 3 alone, say where a new refusal belongs and where it must not be
  restated.

## Do not

- **Do not restate `docs/concepts.md`.** That explains the model to a user; this
  explains the code to a contributor. If a paragraph would fit in either, it
  belongs in `concepts.md` and this file links to it.
- **Do not duplicate `release/surface-inventory.md`.** That file is generated
  against and gated by `subtraction_test.go`; a hand-written copy of it would
  drift and would be a second source of truth about what exists.
- **Do not introduce new nouns.** The dead-vocabulary gate covers agent- and
  user-visible surface, but the `AGENTS.md` vocabulary table is the house
  language and this document should read as though it were gated too.
- **Do not describe planned architecture.** Only what is in the tree. Deferred
  work belongs in `release/release-decision.md` under deferred domains.

## Deferred

Diagrams. An ASCII layer diagram in section 2 is enough; rendered diagrams add a
build step and a way for the picture to disagree with the text.

## Acceptance note — 2026-08-06

`ARCHITECTURE.md` has the nine planned sections, and the relative-link release
gate covers it in the green suite. Every rule in section 3 names its enforcing
test — `TestSurfaceOwnership`, `TestAgentJSONGolden`,
`TestReportsHumanAndJSONAgree`, `TestMaturityGateBites`, and the named refusal
tests — and section 9 states reversal conditions rather than planned work.
