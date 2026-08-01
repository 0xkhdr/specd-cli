# Concepts

This page defines specd's model in the order you encounter it. Use these terms
consistently in plans, code, and documentation. For files on disk, read
[Layout](layout.md). For exact command contracts, read generated
[Operations](operations.md).

## The split

The agent reasons about the change, design, and implementation. The harness
owns the facts that must remain deterministic: lifecycle, validation, approval,
file scope, evidence, and completion.

Nothing in the enforcement path calls an LLM or the network. It is a release
gate that no deterministic package imports either.

## Root

One project directory holding a managed `.specd/` tree. You select it
explicitly — with a positional path or `--root`, exactly one of the two, never
inferred from the working directory or an ambient environment variable. If the
path is not an existing directory, the operation refuses.

`init` creates or adopts the root. Everything else requires it.

## Spec

Accepted current behavioral truth, one file per capability, at
`.specd/specs/<capability>/spec.md`. A spec is what the system does today, not
what you would like it to do. It is written by `sync`, not by hand — you get
there by making a change and having a human accept it.

A greenfield project starts with no specs. That is correct: specd is
brownfield-first, and you document a capability when you change it.

## Change

Proposed work, at `.specd/changes/<name>/`. The name is lowercase kebab-case
and cannot collide with a reserved segment or with an already-archived change.
A change carries four authored artifacts and one harness-owned state file.

### Proposal

`proposal.md` — why the change exists. Five required sections: **Problem**,
**Outcome**, **Scope**, **Non-goals**, **Affected capabilities**. Non-goals are
not filler; they are the thing that keeps a change from growing while it is
being implemented.

### Delta spec

`specs/<capability>/spec.md` inside the change — the behavior this change adds,
modifies, or removes, expressed as requirement deltas under `## ADDED
Requirements`, `## MODIFIED Requirements`, or `## REMOVED Requirements`. Each
requirement is a `### Requirement:` heading with MUST-shaped text and at least
one `#### Scenario:` written as WHEN/THEN.

Behavior first. A requirement describes something observable; if you cannot
write a scenario for it, it is a design note, not a requirement.

The delta is a delta. It never restates the accepted spec — `sync` applies it.

### Design

`design.md` — seven required sections: **Boundaries**, **Interfaces**,
**Invariants**, **Failure behavior**, **Integration**, **Alternatives**,
**Owner**. Sections cite requirements as `capability/Requirement: <text>`, which
is how the design is checked against the delta rather than trusted.

### Tasks

`tasks.md` — one Markdown table, exactly seven columns:

```text
| id | role | files | depends-on | refs | verify | acceptance |
```

Each row is one atomic unit of work: the files it may touch, the tasks it
depends on, the requirements it implements, the command that proves it, and
what counts as accepted. `files` is not documentation — it is the enforced
scope. `verify` is not documentation — it is the command that actually runs.

`role` is `builder`; there is one role until a second one earns its way in.
Dependencies form a DAG. A cycle is a validation failure, not a runtime
surprise.

## Lifecycle

A change is in exactly one of five stages:

| stage | meaning |
| --- | --- |
| `planning` | artifacts are being authored and validated |
| `approved` | a human authorized the current artifact bytes |
| `executing` | tasks are being run against the approved plan |
| `reconciling` | deltas are being accepted into specs |
| `archived` | immutable local history |

There is no sixth stage, no `in_review`, no `abandoned`. Stage lives in
harness-owned `state.json` alongside a monotonic `revision`.

## Four things that are not each other

Keep these four concepts separate.

**Activity** is persisted, harness-owned task state: `pending`, `in_progress`,
`completed`, `failed`, `blocked`. It only moves through legal transitions —
`pending` can become `in_progress` or `blocked`, never `completed` directly.

**Readiness** is derived, never stored. A task is ready when its dependencies
are complete, the plan is valid, and the change is approved. `next` projects
the ready frontier, grouped into waves by dependency depth, so concurrent work
is visible rather than guessed at.

**Evidence** is an observation. `verify` runs the task's declared command in a
bounded process — no shell, a wall-clock timeout, an output byte limit — and
records the exit code, digests, and the Git HEAD it ran at. Passing evidence is
a fact about a moment. It is not permission to close anything.

**Approval** is a human act. `approve` hashes every covered artifact and stores
an aggregate hash. Edit one byte of an approved artifact and the approval is
stale — not silently re-derived, refused. `sync` is the second human gate:
accepting a delta as truth is a separate authorization from accepting the plan.

## Authority

Every operation declares whether a human, an agent, or either may run it. The
human route is derived from a termios ioctl on stdin, so it requires a real
controlling terminal; every other stdin derives `agent`. An agent cannot
approve its own plan, and there is no flag that changes this.

Be honest about what that buys you: the harness can refuse an agent that
declared itself, and it can decline to attest a human. `SPECD_ROUTE` is a host
declaration — provenance, not proof.

## Attempt, scope, completion

`start` opens an attempt: it binds a clean Git baseline and the revision you
observed, and it refuses if the change moved underneath you.

While the attempt is open, the diff against that baseline must stay inside the
task's declared `files`. Anything outside is refused. Git-ignored files count,
deliberately — honoring `.gitignore` would let an agent write anywhere by adding
an ignore rule. The practical consequence: a dirty working tree blocks the loop
for reasons the plan never mentions.

`complete` is the transition. Inside a guarded transaction it re-checks HEAD,
finds evidence applicable to *this* task contract and command at *this* HEAD,
and only then closes the task and bumps the revision. Evidence for a different
command, an older HEAD, or a task whose contract changed does not apply.

## Reconciliation and archive

`sync` applies the approved deltas into accepted specs as one transaction:
either every file lands or none does. `archive` then moves the change to
`.specd/archive/<date>-<name>/`, and refuses rather than overwrite an existing
target.

## Refusals

Every refusal fails closed and carries a code, the offending path, a reason, and
**exactly one legal next action**. Stale state, malformed artifacts, an
unsupported schema version, a corrupt record, an ambiguous root — all of them
stop instead of guessing.

This is the part worth internalizing before you write agent tooling around
specd: a refusal is a routing instruction, not an error to retry.

## What is deliberately absent

No orchestration loop, no daemon, no telemetry, no plugins, no second adapter,
no config file — the entire configuration surface is the flags in
[operations.md](operations.md). Each of these is deferred behind a recorded
threshold, not forgotten. `friction` is how you argue for one: record what a
missing capability actually blocked, in the moment it blocked you.

Next: read [The execution loop](the-loop.md) to apply this model to one task.
