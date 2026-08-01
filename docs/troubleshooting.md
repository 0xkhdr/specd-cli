# Troubleshooting

Every specd refusal has the same shape: a **code**, the offending **path**, a
**reason**, and exactly **one legal next action**. The next action is printed —
in the `next:` line and in the `fix:` clause. Do that. This page exists to
explain *why* it is the only one.

```text
error <code>: <reason>; fix: <next action>
next: <classification> owner=<who> reason=<why>
exit: 2 refusal
```

Exit codes: **0** success, **1** failure (the operation ran and the answer is
no), **2** refusal (the operation would not run at all).

`owner` tells you who has to act — `author` (fix the plan or the code), `human`
(a person at a terminal), `harness` (nothing to do; specd is already handling
it).

## The ones you will actually hit

| what you see | what happened | do this |
| --- | --- | --- |
| `human_approval_required` | `approve` from anything but a real terminal | hand off to a human; there is no bypass |
| `human_operation_required` | same, for `sync` | same |
| `approval_identity` | `--approver` doesn't match git `user.email` / `SPECD_APPROVER` | drop the flag, or match it exactly |
| `scope_outside` | your diff touched a file the task didn't declare | revert the extra file, or amend the plan and re-approve |
| `attempt_dirty` | the Git worktree isn't clean at `start` | commit or clean, then retry |
| `stale_revision` | you passed a `--revision` the change has moved past | run `status`, retry with the revision it reports |
| `complete_evidence` | no applicable passing evidence for this task | run `verify` again at the current HEAD |
| `complete_head` | HEAD moved between `verify` and `complete` | regenerate context and re-verify at current HEAD |
| `context_budget_exceeded` | required context doesn't fit `--budget-bytes` | raise the budget, or split the task |
| `approval_stale` / `approval_drift` | a planning artifact changed after approval | `check`, then get fresh human approval |

Two of these deserve a sentence more.

**`scope_outside`** counts git-ignored files deliberately — otherwise an agent
could write anywhere by adding an ignore rule. Stray build output blocks the
loop. Also note that deletions and renames are refused even inside declared
files: only additions and modifications are in scope.

**`stale_revision`** is not a lock error. It means the change genuinely moved,
and acting on your stale view would have been wrong. The refusal tells you the
revision to retry from.

## Root and paths

| code | meaning | next action |
| --- | --- | --- |
| `root_selection` | zero or two project paths given | supply exactly one |
| `root_invalid` | the path isn't an existing directory | supply one existing project path |
| `not_initialized` | no `.specd` under the root | run `init` |
| `path_escape` | a managed target resolves outside `.specd` | choose another name |
| `path_symlink` | a symlink in the managed prefix | choose another name |
| `path_unsafe` | `.specd` is not a safe directory | repair the path, re-init |
| `unsafe_segment` | name isn't lowercase kebab-case | rename |
| `reserved_segment` | name is one of `archive`, `changes`, `evidence`, `history`, `specs`, `state` | rename |
| `change_not_found` | no such change | choose an existing change |
| `duplicate_change` | the name exists, active or archived | choose another name |

The root is never inferred. If you don't pass one, that is the error, not a
default.

## Planning and validation

`check` reports **findings**, not refusals: exit `1` with a `file:line`, a
message, and a `fix:` per finding. The scaffold fails its own gates on purpose —
placeholder text is a finding.

| code | meaning | next action |
| --- | --- | --- |
| `plan_invalid` | `tasks.md` doesn't parse, or the graph isn't a DAG | repair tasks, run `check` |
| `task_unknown` | the task id isn't canonical | choose a task `status` reports |
| `scaffold_failed` | templates couldn't be written | repair the path, retry |
| `check_state` | the change's state won't decode | choose a change with valid state |
| `check_registry` / `check_policy` | gate registry or policy metadata is wrong | use the canonical default |

Cheapest lesson in this file: **`files` and `depends-on` split on `;`, not
commas.** A comma list is one path with a comma in its name. It passes `check`
and `start` and dies at `verify` as `scope_outside`.

## Approval

| code | meaning | next action |
| --- | --- | --- |
| `human_approval_required` | not a human route | hand off to a human terminal |
| `approval_identity` | claimed approver ≠ trusted identity, or sources disagree | drop `--approver`, or make git `user.email` and `SPECD_APPROVER` agree |
| `approval_intent` | terminal confirmation not given | confirm in the human terminal |
| `approval_gates` | blocking gate findings still open | repair findings, rerun `check` |
| `approval_drift` | artifacts changed between `check` and `approve` | rerun `check`, approve fresh content |
| `approval_stale` | the approval no longer covers current bytes | rerun `check`, obtain fresh approval |
| `approval_artifact` | a covered artifact is missing or unsafe | restore it, rerun `check` |
| `approval_recovery` | an interrupted approval is pending | let the same approver finish it, or rerun `check` |
| `lifecycle_transition` | the change isn't in a stage this operation allows | do the next legal action |

If an approval went stale and you don't know which file moved, `status` names
the blocking artifact and the reason code — see the staleness table in
[approval-and-evidence.md](approval-and-evidence.md).

## Readiness and tasks

| code | meaning | next action |
| --- | --- | --- |
| `change_not_approved` | no current approval | get one |
| `dependency_incomplete` | a predecessor isn't complete | complete it first |
| `task_not_ready` | the task isn't on the frontier | run `status`, choose a ready task |
| `task_transition` | illegal activity transition | run `status` |
| `task_evidence_required` | completion attempted without current passing evidence | run `verify` |
| `task_interrupted` / `task_recovery` | a transition was interrupted | run `status` to recover |

## Context

| code | meaning | next action |
| --- | --- | --- |
| `context_budget_exceeded` | required inputs exceed the budget | raise `--budget-bytes` or split the task |
| `context_approval_stale` | approval isn't current | revise the plan, re-approve |
| `context_frontier_mismatch` | the task isn't on the current frontier | regenerate context |
| `context_snapshot_mismatch` / `context_snapshot_stale` | plan, root, or revision moved | regenerate context |
| `context_requirement_missing` | a `capability/Requirement:` ref doesn't resolve | fix the ref, re-approve |
| `context_task_invalid` | the task is absent or duplicated | repair `tasks.md` |
| `context_scope_invalid` | a declared write path intersects managed state | remove `.specd`/`.git` paths |
| `context_selector_empty` | a selector matched no files | fix the selector |
| `context_selector_unbounded` | a selector exceeds the byte bound | narrow it |
| `context_path_unsafe` / `context_output_unsafe` | a declared path isn't one exact safe path | resolve the named path |

Almost every context refusal means *the world moved since you were told what to
read*. Regenerating is not a workaround; it is the fix.

## Attempts and scope

| code | meaning | next action |
| --- | --- | --- |
| `attempt_dirty` | worktree not clean | clean it, retry |
| `attempt_commitless` | HEAD doesn't resolve to a commit | commit first |
| `attempt_git` | root isn't a Git worktree root | init and commit at the selected root |
| `attempt_exists` | the task already has a bound attempt | finish or continue it |
| `attempt_not_ready` | the task isn't ready | choose a ready task |
| `attempt_scope` | declared files include managed paths, duplicates, or symlinks | fix `tasks.md`, re-approve |
| `attempt_interrupted` / `attempt_recovery` | an attempt was interrupted | run `status` to recover |
| `scope_outside` | changed paths exceed declared authority | revert, or amend the plan and re-approve |
| `scope_drift` | approval, contract, revision, or baseline HEAD moved | regenerate context; re-approve if the plan changed |
| `scope_history` | the attempt isn't the latest transition | run `status`, repair attempt history |
| `scope_git` | Git state unreadable | repair Git, regenerate context |

`scope_drift` is the one that surprises people: it fires when you edit
`tasks.md` during an attempt. That is scope creep being refused, working as
designed.

## Verification and completion

| code | meaning | next action |
| --- | --- | --- |
| `complete_evidence` | no applicable passing evidence | run `verify` |
| `complete_head` | HEAD moved | regenerate context at current HEAD |
| `complete_approval` | approval went stale | rerun `check`, re-approve |
| `complete_activity` | the task isn't in progress | run `status` |
| `complete_command` | the verify command changed | repair the plan, re-approve |
| `complete_replay` | evidence already consumed, or task already proven | run `verify`, or `status` |
| `complete_interrupted` | completion was interrupted | retry at the same revision |
| `complete_recovery` | a pending completion doesn't match your request | run `status` |
| `production_evidence` | a required production check has no evidence | run `verify` for that check |

Non-obvious cases that look like bugs and aren't:

- **Exit `126` with `zero_match: true`** — your test command matched zero tests.
  It exited 0 while proving nothing. Fix the selector.
- **Exit `124`** — the run hit `--timeout`. **Exit `130`** — interrupted. Both
  clear `non_vacuous`, so neither completes anything.
- **A passing `verify` that won't complete** — evidence identity covers change,
  task, attempt, HEAD, task hash, command hash, approval hash, and revision. One
  mismatch and it doesn't apply.

## Sync and archive

| code | meaning | next action |
| --- | --- | --- |
| `human_operation_required` | `sync` from a non-human route | hand off |
| `sync_lifecycle` | the change isn't approved | run `status` |
| `sync_plan` | task graph invalid | repair tasks, run `check` |
| `sync_blocked` | unresolved conflicts between deltas and accepted specs | resolve them |
| `sync_drift` | an accepted spec changed after it was synced | plan the rest as a new change |
| `sync_path` | a managed path escapes `.specd` | restore `.specd` from version control |
| `archive_incomplete` | tasks remain | complete them |
| `archive_unsynced` | not reconciled | `sync` first |
| `archive_target_exists` | that dated archive path already exists | inspect the existing archived change |
| `archive_approval` / `archive_evidence` | artifacts or evidence changed after sync | restore from version control, or plan a new change |

Archive never deploys, commits, pushes, or opens anything. It moves a directory.

## State, records, locks

| code | meaning | next action |
| --- | --- | --- |
| `stale_revision` | your `--revision` is behind | retry from the revision reported |
| `state_corrupt` | `state.json` won't decode | repair outside automated mutation |
| `state_schema_unsupported` | written by a newer specd | upgrade, or repair |
| `state_identity_mismatch` | `change` field ≠ directory name | repair |
| `*_history` / `*_evidence` (incomplete tail) | an append was interrupted | run `status`; restore from version control if it persists |
| `record-revision-chain` | history's revisions for a change are not contiguous | restore the change and its history from version control, or choose a name with no recorded history |
| `lock_busy` | another operation holds the change or root lock | wait, retry |
| `lock_failed` | the lock could not be taken | retry |

`record-revision-chain` has one cause that is not corruption: **a change
directory was deleted while its history records remain.** History is append-only, so the `created` record survives, and
creating a change with the same name again starts from revision 0 against a
chain that already reached 1.

There is no operation that abandons a change — a change leaves `planning` only
by being approved, executed, synced, and archived. If you delete one anyway,
that name is unusable in that root until you restore the directory from version
control. Pick a different name, or restore the change and finish it.

An incomplete tail is normally invisible — the ledgers are replayed and
recovered on read (journey 04). You only see these codes when the file was
damaged by something other than specd.

`state_*` refusals all say "repair corrupt state outside automated mutation"
and mean it. No specd operation will rewrite corrupt state for you, because a
harness that repairs its own truth silently isn't one.

## Review, policy, reports, friction

| code | meaning | next action |
| --- | --- | --- |
| `review_identity` | claimed reviewer ≠ trusted identity | match it |
| `review_verdict` | the reviewer is the approver or the implementer | get a third person |
| `review_policy` | the current policy declares no review check | select a policy that does |
| `review_subject` | the attempt doesn't match | regenerate context, start again |
| `policy_unknown` | unknown profile | `default` or `production` |
| `policy_drift` / `policy_transition` | the policy changed without a recorded human transition | record one explicitly |
| `report_kind` | unknown `--kind` | pick one of the four |
| `friction_hypothetical` | you recorded friction for a task `status` doesn't report as blocked | only record what actually blocked you |
| `friction_unknown` | the task isn't canonical | record against a real task |
| `friction_future` | the record is stamped ahead of the clock | fix the system clock |

`friction_hypothetical` is the interesting one: friction records are the only
route to unblocking a deferred domain, so they are only accepted for work that
was genuinely stopped. You cannot pre-argue for a feature.

## Still stuck

1. Run `specd status <change>`. It reports lifecycle, approval freshness,
   activity counts, the frontier, and the next action, all derived fresh.
2. Add `--json` to get the same facts as a stable envelope.
3. Read the `next:` line literally. If it says `owner=human`, no amount of
   agent-side retrying will help.

If the refusal is genuinely wrong — the state is fine and specd disagrees —
that's a bug worth reporting with the full output, the change's `state.json`,
and the tail of `history.jsonl`.
