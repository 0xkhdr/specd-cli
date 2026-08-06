# Development TODO

This file records proposed development work. It is not shipped behavior and
does not authorize implementation. Each item must become a specd change and
receive human approval before code is edited.

## 1. Reopen a change that reached a dead end

### Problem

An approved change has no legal path back to planning. If its plan is wrong, or
an attempt is bound to a baseline that can no longer be verified, the agent can
only restore the exact approved bytes or create another change containing the
same proposal, requirements, design, and tasks. The duplicate preserves neither
the original change identity nor a machine-checked explanation of why execution
stopped.

This is the failure described by limitations 6 and 8 in
`release/release-decision.md`. The recovery must fix those dead ends without
weakening approval, evidence, authority, scope, staleness, or append-only
history.

### Required outcome

Add one `specd reopen <change> --revision <observed> --reason <reason>`
operation. It returns the existing change to `planning`, so its authored
artifacts can be repaired and passed through `check` and human `approve` again.
It does not create or copy a change.

`reopen` is agent-callable because it only removes existing execution
authority. It must never approve content, make evidence applicable, complete a
task, reconcile accepted specs, or mutate project files.

### Transition contract

In one change-locked, revision-checked transition, `reopen` must:

1. accept only an active change in `approved` lifecycle;
2. accept current or stale plan approval, because stale plan bytes are one of
   the states this operation exists to recover;
3. refuse while project files contain uncommitted attempt work, with exactly
   one legal next action to make the project worktree clean before retrying;
4. append one typed `reopened` history record containing the actor, reason,
   prior lifecycle, prior revision, prior approval identity, active attempt
   identity when present, and observed HEAD;
5. atomically move the lifecycle to `planning`, clear all task activity and
   active attempt bindings, increment the revision once, and point
   `last_transition` at that record;
6. retain all earlier approval, attempt, evidence, completion, and friction
   records unchanged; and
7. return one next action: repair the existing planning artifacts and run
   `specd check <change>`.

Clearing task activity is deliberately conservative. A new approval covers a
new whole-plan identity, so no completion from the old plan is silently carried
into it. Existing project commits remain in Git and may make a repeated task a
no-op, but the new task still needs current evidence before completion.

### Boundaries and non-goals

- Reuse the existing lifecycle, record ledger, revision guard, change lock,
  atomic state replacement, Git inspection, refusal, registry, dispatch, and
  output owners.
- Do not add a second lifecycle, parser, state file, recovery ledger, or output
  renderer.
- Do not add `--force`, a target-lifecycle flag, or any approval/evidence
  bypass.
- Do not run `git reset`, delete working files, remove the change directory, or
  truncate history or evidence.
- Do not reopen `reconciling` or `archived` changes. Accepted truth is handled
  by a new change, not by reversing sync or archive.
- Do not reuse approval-transition validation for this reverse edge. Approval
  remains forward-only; reopening is a separate, typed revocation transition.

### Failure behavior and bite tests

The implementation is incomplete until tests construct each forbidden state
and assert the stable refusal code, unchanged managed bytes, and one legal next
action.

| case | required result |
| --- | --- |
| stale `--revision` | refuse; report the current revision |
| lifecycle is `planning` | refuse; continue authoring and run `check` |
| lifecycle is `reconciling` or `archived` | refuse; plan any further behavior as a new change |
| project work from an active attempt is dirty | refuse; make the project worktree clean, then retry |
| history or state is malformed/ambiguous | fail closed through the existing owner; write nothing |
| two callers reopen the same revision | exactly one wins; the other receives `stale_revision` |
| append succeeds but state replacement is interrupted | replay recovers the same transition; no second record is invented |
| prior evidence exists | retain it, but never treat it as applicable to the reopened plan |
| agent invokes `approve` after reopening | the existing human gate still refuses it |

Add one retained CLI journey covering an approved change with an active
attempt, a moved HEAD, and no dirty project files. The journey must prove that
the same change identity returns to planning, old records remain readable, the
attempt is no longer active, tasks project as pending, and the only route back
to execution is `check` followed by human approval.

### Implementation plan

| task | work | verification |
| --- | --- | --- |
| R1 | Add the canonical `reopened` record payload and the core transition, including revision, lifecycle, dirty-project, replay, and atomicity checks. | Focused core tests for the success path and every refusal above. |
| R2 | Register `reopen`, bind one command handler, and project its result through `internal/cmd/output.go`. | Registry, dispatch, JSON golden, text/JSON parity, and agent-contract tests. |
| R3 | Add the retained journey and surface ownership, update the owning lifecycle, troubleshooting, release-limit, and changelog documentation, then regenerate operation docs. | `make docs`, affected package tests, then `make ci`. |

Before implementation, create a specd change whose proposal and design resolve
the remaining naming and exact file-scope details. If source inspection shows
that a smaller transition can satisfy every invariant and bite case, prefer it;
do not split `reopen` into multiple operations without a failing requirement.

## Later items

Add another numbered item only after a concrete failure or requirement proves
it belongs in the product.
