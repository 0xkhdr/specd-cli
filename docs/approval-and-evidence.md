# Approval and evidence

Approval, evidence, and completion answer different questions. Keeping them
separate is the core safety property of specd. Release journeys `01`–`14` in
`internal/integration/release_journeys_test.go` replay these boundaries.

## Verification is not completion

`verify` records an observation. `complete` closes a task. They are separate
operations because they answer separate questions: *did this command pass just
now*, and *may this task be considered done*.

A passing `verify` gives you a record in `.specd/evidence.jsonl`. It changes no
lifecycle state, no task activity, no revision. Nothing about the change moves.

`complete` then decides whether any recorded evidence actually applies.

### What "applies" means

`complete` looks for a `TestRun` record whose subject matches the task you are
completing, on **eight** fields, all of them
(`internal/core/evidence/evidence.go`, `TestRun.Matches`):

| field | why it is in the identity |
| --- | --- |
| change | evidence from another change is not yours |
| task id | evidence for another task is not yours |
| attempt id | evidence from an abandoned attempt does not carry over |
| HEAD | evidence observed at a different commit describes different code |
| task hash | the task's contract — files, verify command, refs — must be unchanged |
| command hash | evidence from a *different command* proves a different thing |
| approval hash | evidence gathered under a plan a human has not approved does not count |
| state revision | evidence from before the change moved is stale |

and then requires, on top of matching (`TestRun.Applicable`):

- `Passed`
- `ExitCode == 0`
- `NonVacuous`
- an end timestamp that is not in the future

Change any one of those and completion refuses with `complete_evidence`. Journey
07 replays exactly this: stale evidence after a Git HEAD change.

### Vacuous evidence proves nothing

A test command that matches zero tests exits 0. It is a green result that
asserted nothing, and it is the easiest way to fake progress without lying.

specd detects it for Go test invocations, sets `zero_match: true`, and rewrites
the exit code to `126` (`ZeroMatchExitCode`, `internal/core/verify/run.go`).
`Passed` requires `NonVacuous`. Journey 09 replays failing and zero-match runs
together for this reason.

Two other synthesized exit codes: `124` for a run that hit `--timeout`, `130`
for an interrupted one. Both clear `NonVacuous` too.

### Completion is a transaction

Inside the guarded completion path, specd re-reads HEAD and refuses with
`complete_head` if it moved between your verification and your completion
(`internal/core/complete.go:198`). Then it consumes exactly one evidence
identity, appends a `completion` history record, and bumps the revision — or it
does none of that.

`--revision` is your part of the guard: you pass the revision you observed, and
if the change moved underneath you, completion refuses instead of acting on a
stale view.

### There is no bypass

There is no `--force`, no `--skip-verify`, no `--assume-passed`, and no
environment variable. This is not an oversight to be reported; it is the
product. Absence of a bypass is asserted by the release gate, and adding one is
listed as forbidden in the project's own build rules.

If an agent cannot complete a task, the correct outcomes are: fix the code and
verify again, or amend the plan and get it re-approved. Not both, and never
neither.

## An agent cannot approve its own plan

`approve` and `sync` are human-only. Two different authorizations:

- **`approve`** — a human accepts *the plan*: this proposal, this delta, this
  design, these tasks, as they are written right now.
- **`sync`** — a human accepts *the resulting behavior* as truth, applying the
  delta into `.specd/specs/`.

Passing the first does not pass the second. An implementation that works is not
the same claim as a behavior worth keeping.

### How the human route is derived

From a termios ioctl on stdin. Only a real controlling terminal derives
`human`; every other stdin — pipe, agent, CI runner, editor shell without a tty
— derives `agent` and is refused:

```text
error human_approval_required: approve is human-only and cannot use an agent-capable route
next: human_handoff owner=human reason=approval is human-only; hand off approval to a human terminal
exit: 2 refusal
```

Journey 12 replays this handoff. When an agent hits it, the correct behavior is
to stop and tell the human what to run — not to retry, not to look for another
route.

Be precise about what this buys you. `SPECD_ROUTE` is a host declaration:
provenance, not proof. The harness can **refuse** an agent that declared itself;
it cannot **attest** that a human is present. A host that lies about its route
is a host you already trusted with your source tree.

### Identity is derived, not claimed

The trusted approver identity is Git `user.email`, or `SPECD_APPROVER`. If both
are set they must agree. Claim something else with `--approver` and you get:

```text
error approval_identity: claimed approver differs from trusted identity
```

Omitting `--approver` is usually right because identity is derived. The flag
checks a claim; it does not choose an identity. Approval and sync still require
a non-empty `--reason`.

## Approval goes stale

An approval covers artifact bytes, not intentions. `specd approve` hashes every
covered planning artifact and stores per-file hashes plus an aggregate.

Any of these makes it stale (`internal/core/approval_status.go`), and each has
its own reason code so you can tell what happened:

| reason | what changed |
| --- | --- |
| `approval_missing` | never approved |
| `approval_not_applicable` | the lifecycle stage no longer matches the approval |
| `state_revision_changed` | task activity moved after the approval |
| `registry_version_changed` | the gate registry the approval was taken under changed |
| `policy_digest_changed` | the policy the approval was taken under changed |
| `artifact_set_changed` | an artifact was added or removed from the covered set |
| `artifact_missing_or_unsafe` | a covered artifact is gone or is no longer a safe path |
| artifact hash mismatch | a covered artifact's bytes changed |

Journey 06 replays the byte-change case. The recovery is always the same shape
and always stated in the refusal: rerun `check`, then obtain fresh human
approval.

The consequence worth planning around: **you cannot quietly widen a task's
declared files mid-implementation.** Editing `tasks.md` invalidates the approval
and the in-flight attempt (`scope_drift`, "task contract no longer matches
attempt"). Scope creep costs a human round trip, on purpose.

## Review is a third pair of eyes

`review` records a separate reviewer verdict, and refuses when the reviewer
identity equals the recording actor: *"review evidence requires a separate
reviewer identity"* (`internal/core/evidence/production.go:246`). An agent
cannot review itself any more than it can approve itself.

Review is part of the opt-in production profile. That profile is experimental
and carries no production assurance in the current release decision. Review is
not required by the default loop and does not substitute for either human gate.

## Reports authorize nothing

`report` and `review` without `--verdict` are read-only projections. Recording a
review verdict writes review evidence, but it still authorizes nothing. Reports
derive from state and evidence; they never move a lifecycle.

## The short version for agent authors

1. Never treat a passing `verify` as done. Call `complete`.
2. Never retry past a `human_approval_required` or `human_operation_required`
   refusal. Surface it and stop.
3. Never edit `tasks.md`, `proposal.md`, `design.md`, or the delta spec during
   an attempt. Finish, or hand back for re-approval.
4. Pass the `--revision` you actually observed. It is a guard, not a formality.
5. Read the `next:` line. After a refusal there is exactly one legal action, and
   it is printed.

Next: agent integrations should read [Driving specd from an agent](agent-setup.md);
operators can use [Troubleshooting](troubleshooting.md) for refusal recovery.
