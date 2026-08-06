# Design

## Boundaries
lifecycle-recovery/Requirement: Reopen an approved change
lifecycle-recovery/Requirement: Refuse unsafe recovery
lifecycle-recovery/Requirement: Preserve append-only truth

The core owns the decision under the existing change lock. It reads canonical
state and history, checks the caller's observed revision, verifies the
lifecycle and Git worktree, appends one typed record, and atomically replaces
state. The command layer only adapts arguments and projects the result through
the shared envelope.

## Interfaces
`specd reopen CHANGE --revision REVISION --reason REASON` is agent-visible
and state-writing. The result identifies the same change, prior approval and
attempt when present, the before and after revisions, the observed HEAD, and
the appended history record. Its only successful next action is to repair the
planning artifacts and run `specd check CHANGE`.

## Invariants
Only lifecycle `approved` can reopen. Current or stale approval is accepted
because the operation revokes authority rather than granting it. The expected
revision has one winner. Project work must be clean, while harness-managed
changes are ignored. The transition clears all task activity and active
attempt bindings, increments revision once, and points `last_transition` at
the new record. Prior approval, attempt, evidence, completion, and friction
records are never rewritten or removed. Approval remains human-only and the
reopened bytes require a new approval identity.

## Failure behavior
Stale revision reports the current revision. Planning directs the author to
continue authoring and check; reconciling or archived directs planning a new
change. Dirty project work directs cleaning the worktree. Malformed or
ambiguous state and history fail closed through their existing owners. Every
refusal writes no managed bytes and carries exactly one legal next action.
If history append succeeds but state replacement is interrupted, retry finds
the matching pending record and completes the same transition without adding a
second record.

## Integration
Reuse the lifecycle constants, revision guard, change lock, record codec and
replay, atomic replacement, Git inspection, operation registry, generic flag
parser, dispatch map, shared output projection, release journey runner, and
surface inventory. The new transition is separate from forward-only approval
validation.

## Alternatives
Allowing `check` in approved does not restore authoring or revoke authority.
Copying into another change loses identity and history. A general reverse
transition, force flag, cancel operation, recovery ledger, or new lifecycle is
more surface than the observed dead end requires.

## Owner
`internal/core` owns recovery and `internal/core/record` owns the typed payload;
`internal/cmd` owns dispatch and both output projections.
