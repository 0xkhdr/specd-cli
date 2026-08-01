# The execution loop

What happens between an approved plan and a completed task, and why each step
refuses what it refuses.

```text
next → context → start → edit declared files → verify → complete
```

Five operations, run once per task. [getting-started.md](getting-started.md)
walks them once with real output; this page is the depth behind them. For the
approval and evidence rules the loop enforces, read
[approval-and-evidence.md](approval-and-evidence.md).

## `next` — what may be worked

`next` derives readiness from `tasks.md` plus harness-owned activity in
`state.json`. Nothing about readiness is stored, so it cannot go stale or
disagree with the plan.

Each task gets one readiness value (`internal/core/readiness.go`):

| readiness | meaning |
| --- | --- |
| `ready` | dependencies complete, plan valid, change approved — workable now |
| `waiting_dependency` | a predecessor is not complete |
| `waiting_approval` | the change is not approved, or the approval is stale |
| `active` | an attempt is open on it |
| `terminal` | completed |
| `blocked` | explicitly blocked |

The **frontier** is every `ready` task, not one task. Two tasks with no
dependency between them are both on the frontier, and you may work them
concurrently — the harness serializes managed writes with a per-change lock, so
concurrency is a real option rather than a race.

### Waves

Each task carries a wave number: the longest dependency path to it
(`internal/core/taskgraph.go`). Wave 0 depends on nothing, wave 1 depends only
on wave 0, and so on. Waves are the shape of the plan, not a schedule — nothing
forces you to drain a wave before starting the next task that is ready.

A dependency cycle is not a wave; it is `plan_invalid`, caught by `check` before
approval.

### When nothing is ready

`next` returns a classification and, when work is stopped, exactly one blocker:

| classification | you are here because |
| --- | --- |
| `frontier` | there is workable work; the ids are listed |
| `all_complete` | every task is completed |
| `waiting_approval` | `change_not_approved` or `approval_stale` |
| `waiting_dependency` | `dependency_incomplete` |
| `plan_invalid` | `tasks.md` does not parse or the graph is not a DAG |
| `task_blocked` | something else, named by the blocker code |

Each blocker names an `owner` — `author`, `human`, or `harness` — which is who
has to act. An agent that gets `owner=human` is supposed to stop.

A **failed** task can be retried: it returns to the frontier when its
dependencies are still complete (`CanRetry`). Failure is not terminal, and it is
also not silently forgotten.

## `context` — bounded read input

`context` assembles what you need to do exactly one task: its requirement, its
design boundary, its declared files, its verification command. Not the
repository.

The point is not token thrift. It is that a task's inputs are *declared and
hashed*, so what an implementer read is a fact rather than a claim. The manifest
carries a `manifest_hash`, an `approval_hash`, and a `frontier_hash`, and
`start` binds them.

### The budget

`--budget-bytes` caps the assembled context. Required inputs and optional
inputs are treated differently:

- optional items are dropped first, each omission recorded with a reason and
  counted in `omission_count` — dropped, never silently truncated mid-file;
- if the **required** inputs alone exceed the budget, `context` refuses with
  `context_budget_exceeded` rather than handing you a partial view of what you
  were supposed to read.

A refusal here is usually a signal about the plan, not the budget: a task whose
required reading does not fit is a task that is doing too much.

### Context refuses stale views

`context` fails closed on `context_approval_stale` (the approval is not
current), `context_snapshot_mismatch` and `context_snapshot_stale` (the plan,
root, or revision moved under it), `context_frontier_mismatch` (the task is not
on the frontier you were shown), and `context_dependency_missing`. Regenerating
context is always the recovery, and it is always in the `next:` line.

## `start` — bind an attempt

`start` opens exactly one attempt on one task, and writes an `attempt` history
record binding:

- the **revision** you observed, passed as `--revision`;
- the **baseline HEAD**, a real commit;
- the **declared files**, resolved from the task row;
- the **approval hash** and the task **contract hash**;
- a **state guard hash** over the state fields the attempt does not own.

If any of these no longer match when you verify, the attempt is dead and you get
a `scope_drift` refusal naming which one moved. That is the point: an attempt is
a claim about the world at a moment, and the world is checked, not assumed.

Declared files are validated at `start`: no `.specd` or `.git` paths, no
duplicates, no symlinks, no aliases into the managed tree
(`internal/core/attempt.go`). They are **not** checked for existence — a
misspelled or mis-delimited path is accepted here and surfaces as a scope
refusal at `verify`.

> Lists in `tasks.md` split on `;`. A comma-separated `files` cell becomes one
> path with a comma in its name, passes `check` and `start`, and fails at
> `verify`.

## Edit — the scope rule

While an attempt is open, your diff against the baseline must stay inside the
declared files. `verify` derives changes with `git diff --name-status` against
the baseline, plus untracked files, plus **ignored** files
(`internal/core/scope.go`).

Anything outside is `scope_outside`:

```text
error scope_outside: … changed paths exceed exact attempt authority: …
next: blocked owner=author; revise the plan and obtain fresh human approval
```

Three consequences worth knowing before they surprise you:

1. **Git-ignored files count.** Deliberately: honoring `.gitignore` would let an
   agent write anywhere by adding an ignore rule. Stray build output blocks the
   loop for reasons your plan never mentions. Clean the tree before `start`.
2. **Deletions and renames are not `A` or `M`**, so they are refused as
   offending paths even inside declared files. A task that deletes a file is a
   plan-level decision.
3. **Widening scope mid-attempt is not possible.** Editing `tasks.md` changes
   the contract hash, which kills the attempt *and* the approval. Scope creep
   costs a human round trip, on purpose.

Managed `.specd/` paths the operation itself wrote are allowed; lock files are
ignored.

### Honest boundary

The harness *validates* scope. It does not *contain* a process. Nothing stops a
misbehaving tool from writing outside the declared files — specd will refuse the
attempt afterwards, which is detection, not prevention. Real containment is the
host's job, and the assurance label says `advisory` until a host proves
otherwise.

## `verify` — bounded execution

`verify` runs the task's declared command:

- **no shell** by default — the authored command is split into argv and executed
  directly, so nothing is expanded, globbed, or chained. Shell semantics are
  opt-in and explicit: prefix the command with `shell:` in `tasks.md`
  (`verify.ParseCommand`). Either way the exact command is digested into the
  evidence identity, so changing it invalidates the evidence;
- **a wall-clock timeout** (`--timeout`), synthesizing exit code `124`;
- **an output limit** (`--output-limit`, max 1 MiB), recording digests plus
  bounded excerpts and a truncation flag;
- **interruption** recorded as exit `130`.

The result is appended to `.specd/evidence.jsonl` pinned to the current HEAD. It
is an observation. It completes nothing — see
[approval-and-evidence.md](approval-and-evidence.md).

A failing verification is a normal, recorded outcome. It is evidence that
something does not work, which is worth having.

## `complete` — the transition

`complete --revision <observed>` re-checks HEAD, finds evidence applicable to
this exact task contract, command, attempt, approval, and revision, then closes
the task and bumps the revision — atomically, or not at all.

Then you go back to `next`.

## Concurrency and interruption

Managed writes are serialized by a per-change lock, and state writes are
old-or-new: a crashed process leaves the previous bytes, never a half-file. The
two ledgers are append-only and replayed on read, so an interrupted append is
recovered rather than trusted (journey 04).

Practically: kill specd at any point and re-run `status`. It will tell you where
you actually are. There is no "resume" command because there is no
half-transition to resume.

## When you are refused

Every refusal carries a code, the offending path, a reason, and exactly one
legal next action. Exit `2` is a refusal; exit `1` is a failure; exit `0` is
success.

Do what the `next:` line says. If it names `owner=human`, hand off — that is a
real boundary, not a retry.
