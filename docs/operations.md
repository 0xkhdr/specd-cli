# Operations

Generated from the operation registry (schema 1). Do not edit by hand:
run `go test ./internal/core -run TestOperationProjectionParity` to detect drift
and `SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity`
to regenerate this file.

Examples are display data. They are never parsed, expanded, or executed.

## init

| fact | value |
|---|---|
| id | init |
| summary | Create or adopt the managed .specd root and install the agent guidance file. |
| usage | specd init [root] [--root <root>] [--json] |
| actor | either |
| effect | project_write |
| lifecycles | not lifecycle scoped |
| requiresRoot | true |
| requiresChange | false |
| requiresTask | false |
| authoritySource | none |
| scopeSource | root |
| arguments | [root] Project directory to manage. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document. |
| exits | 0 success: operation succeeded; 1 failure: the root could not be created or adopted; 2 refusal: usage error or fail-closed refusal |
| resultType | init_result |
| agentVisible | true |
| executable | true |
| example | specd init |

## new

| fact | value |
|---|---|
| id | new |
| summary | Create a change with its planning artifacts and state. |
| usage | specd new <change> [--root <root>] [--json] [--capability <capability>] |
| actor | either |
| effect | state_write |
| lifecycles | not lifecycle scoped |
| requiresRoot | true |
| requiresChange | false |
| requiresTask | false |
| authoritySource | none |
| scopeSource | change |
| arguments | <change> Change identifier to create. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --capability (string) Capability the delta spec is authored under; the change name when omitted. |
| exits | 0 success: operation succeeded; 1 failure: the change could not be created; 2 refusal: usage error or fail-closed refusal |
| resultType | new_result |
| agentVisible | true |
| executable | true |
| example | specd new safe-create --capability safety |

## check

| fact | value |
|---|---|
| id | check |
| summary | Run planning gates over the change and report findings. |
| usage | specd check <change> [--root <root>] [--json] |
| actor | either |
| effect | read |
| lifecycles | planning, approved, executing, reconciling, archived |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | none |
| scopeSource | change |
| arguments | <change> Change to validate. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document. |
| exits | 0 success: operation succeeded; 1 failure: blocking gate findings were reported; 2 refusal: usage error or fail-closed refusal |
| resultType | check_result |
| agentVisible | true |
| executable | true |
| example | specd check safe-create |

## approve

| fact | value |
|---|---|
| id | approve |
| summary | Record human approval of the current planning artifacts. |
| usage | specd approve <change> [--root <root>] [--json] [--approver <approver>] [--reason <reason>] |
| actor | human |
| effect | state_write |
| lifecycles | planning |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | human_identity |
| scopeSource | change |
| arguments | <change> Change to approve. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --approver (string) Approver identity for non-interactive use.; --reason (string) Reason recorded with the approval. |
| exits | 0 success: operation succeeded; 1 failure: approval was refused or could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | approval_record |
| agentVisible | false |
| executable | true |
| example | specd approve safe-create --approver me@example.com --reason reviewed |

## reopen

| fact | value |
|---|---|
| id | reopen |
| summary | Revoke execution authority and return a change to planning. |
| usage | specd reopen <change> [--root <root>] [--json] --revision <revision> --reason <reason> |
| actor | either |
| effect | state_write |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | actor_identity |
| scopeSource | change |
| arguments | <change> Approved change to return to planning. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --revision (uint, required) State revision observed before reopening.; --reason (string, required) Why execution authority is being revoked. |
| exits | 0 success: operation succeeded; 1 failure: reopening was refused or could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | reopen_result |
| agentVisible | true |
| executable | true |
| example | specd reopen safe-create --revision 4 --reason repair-plan |

## status

| fact | value |
|---|---|
| id | status |
| summary | Report lifecycle, approval, readiness, and next action for a change. |
| usage | specd status <change> [--root <root>] [--json] |
| actor | either |
| effect | read |
| lifecycles | planning, approved, executing, reconciling, archived |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | none |
| scopeSource | change |
| arguments | <change> Change to inspect. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document. |
| exits | 0 success: operation succeeded; 1 failure: the change status could not be projected; 2 refusal: usage error or fail-closed refusal |
| resultType | status_result |
| agentVisible | true |
| executable | true |
| example | specd status safe-create |

## next

| fact | value |
|---|---|
| id | next |
| summary | Project the ready task frontier or the single blocking action. |
| usage | specd next <change> [task] [--root <root>] [--json] |
| actor | either |
| effect | read |
| lifecycles | planning, approved, executing, reconciling, archived |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | none |
| scopeSource | change |
| arguments | <change> Change to project.; [task] Validate one task's frontier membership. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document. |
| exits | 0 success: operation succeeded; 1 failure: readiness could not be projected; 2 refusal: usage error or fail-closed refusal |
| resultType | next_result |
| agentVisible | true |
| executable | true |
| example | specd next safe-create |

## context

| fact | value |
|---|---|
| id | context |
| summary | Assemble the bounded read context for exactly one task. |
| usage | specd context <change> <task> [--root <root>] [--json] [--budget-bytes <budget-bytes>] |
| actor | either |
| effect | read |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | none |
| scopeSource | task_declared_files |
| arguments | <change> Change holding the task.; <task> Task to bound context for. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --budget-bytes (int, default 0) Byte budget for assembled context; 0 is unbounded. |
| exits | 0 success: operation succeeded; 1 failure: context could not be assembled within its bounds; 2 refusal: usage error or fail-closed refusal |
| resultType | context_manifest |
| agentVisible | true |
| executable | true |
| example | specd context safe-create S1-01 --budget-bytes 65536 |

## start

| fact | value |
|---|---|
| id | start |
| summary | Bind a clean Git baseline and open one task attempt. |
| usage | specd start <change> <task> [--root <root>] [--json] --revision <revision> |
| actor | either |
| effect | state_write |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | actor_identity |
| scopeSource | task_declared_files |
| arguments | <change> Change holding the task.; <task> Frontier task to start. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --revision (uint, required) State revision observed before the attempt. |
| exits | 0 success: operation succeeded; 1 failure: the attempt could not be opened; 2 refusal: usage error or fail-closed refusal |
| resultType | start_result |
| agentVisible | true |
| executable | true |
| example | specd start safe-create S1-01 --revision 4 |

## verify

| fact | value |
|---|---|
| id | verify |
| summary | Run the task's declared verification and record evidence at current HEAD. |
| usage | specd verify <change> <task> <attempt> [--root <root>] [--json] [--timeout <timeout>] [--output-limit <output-limit>] |
| actor | either |
| effect | state_write |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | actor_identity |
| scopeSource | task_declared_files |
| arguments | <change> Change holding the task.; <task> Task to verify.; <attempt> Attempt identifier returned by start. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --timeout (duration, default 2m0s) Wall-clock bound on the verification command.; --output-limit (int, default 32768) Byte bound on recorded command output. |
| exits | 0 success: operation succeeded; 1 failure: evidence could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | verify_result |
| agentVisible | true |
| executable | true |
| example | specd verify safe-create S1-01 A1 --timeout 2m |

## complete

| fact | value |
|---|---|
| id | complete |
| summary | Consume applicable passing evidence and close the task. |
| usage | specd complete <change> <task> [--root <root>] [--json] --revision <revision> |
| actor | either |
| effect | state_write |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | actor_identity |
| scopeSource | task_declared_files |
| arguments | <change> Change holding the task.; <task> Task to complete. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --revision (uint, required) State revision observed before completion. |
| exits | 0 success: operation succeeded; 1 failure: completion was refused or could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | completion |
| agentVisible | true |
| executable | true |
| example | specd complete safe-create S1-01 --revision 6 |

## review

| fact | value |
|---|---|
| id | review |
| summary | Record or project one separate reviewer verdict for a task. |
| usage | specd review <change> <task> <attempt> [--root <root>] [--json] [--reviewer <reviewer>] [--verdict <verdict>] [--findings <findings>] |
| actor | either |
| effect | state_write |
| lifecycles | approved |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | actor_identity |
| scopeSource | change |
| arguments | <change> Change holding the reviewed task.; <task> Task the verdict is bound to.; <attempt> Attempt identifier returned by start. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --reviewer (string) Reviewer identity claim; it must match the trusted reviewer identity.; --verdict (string, one of approve/reject) Verdict to record; omitted projects the current review state without writing.; --findings (string) Bounded reviewer findings; required to reject. |
| exits | 0 success: operation succeeded; 1 failure: the verdict was refused or could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | review_result |
| agentVisible | true |
| executable | true |
| example | specd review safe-create S1-01 A1 --reviewer reviewer@example.com |

## sync

| fact | value |
|---|---|
| id | sync |
| summary | Reconcile approved deltas into accepted specs. |
| usage | specd sync <change> [--root <root>] [--json] [--approver <approver>] [--reason <reason>] |
| actor | human |
| effect | state_write |
| lifecycles | approved, reconciling |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | human_identity |
| scopeSource | change |
| arguments | <change> Change to reconcile. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --approver (string) Approver identity for non-interactive use.; --reason (string) Reason recorded with the sync authorization. |
| exits | 0 success: operation succeeded; 1 failure: sync was refused or could not be committed; 2 refusal: usage error or fail-closed refusal |
| resultType | sync_result |
| agentVisible | false |
| executable | true |
| example | specd sync safe-create --approver me@example.com --reason reviewed |

## archive

| fact | value |
|---|---|
| id | archive |
| summary | Validate and move a reconciled change into the archive. |
| usage | specd archive <change> [--root <root>] [--json] |
| actor | either |
| effect | state_write |
| lifecycles | reconciling |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | actor_identity |
| scopeSource | change |
| arguments | <change> Change to archive. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document. |
| exits | 0 success: operation succeeded; 1 failure: archive was refused or could not be committed; 2 refusal: usage error or fail-closed refusal |
| resultType | archive_result |
| agentVisible | true |
| executable | true |
| example | specd archive safe-create |

## report

| fact | value |
|---|---|
| id | report |
| summary | Project one of the four canonical read-only reports. |
| usage | specd report <change> [--root <root>] [--json] --kind <kind> [--profile <profile>] |
| actor | either |
| effect | read |
| lifecycles | planning, approved, executing, reconciling, archived |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | false |
| authoritySource | none |
| scopeSource | change |
| arguments | <change> Change to project. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --kind (string, required, one of status/proof/history/review) Report to project; only these four kinds exist.; --profile (string, one of default/production) Policy profile the report is projected under. |
| exits | 0 success: operation succeeded; 1 failure: the report could not be projected; 2 refusal: usage error or fail-closed refusal |
| resultType | report_result |
| agentVisible | true |
| executable | true |
| example | specd report safe-create --kind status |

## friction

| fact | value |
|---|---|
| id | friction |
| summary | Record one observation that a deferred domain blocked real work. |
| usage | specd friction <change> <task> [--root <root>] [--json] --domain <domain> --blocked-operation <blocked-operation> --consequence <consequence> --revision <revision> |
| actor | either |
| effect | state_write |
| lifecycles | planning, approved, executing, reconciling, archived |
| requiresRoot | true |
| requiresChange | true |
| requiresTask | true |
| authoritySource | actor_identity |
| scopeSource | change |
| arguments | <change> Change holding the blocked task.; <task> Task status reports as blocked. |
| flags | --root (string) Select the project root holding .specd.; --json (bool) Emit the machine-readable result document.; --domain (string, required, one of delivery/maintenance/multi-root/orchestration/references/security-scanners) Deferred domain whose absence caused the block; only these exist.; --blocked-operation (string, required) Operation that was blocked when the observation was made.; --consequence (string, required) What the missing capability prevented, observed and not predicted.; --revision (uint, required) State revision observed before recording. |
| exits | 0 success: operation succeeded; 1 failure: friction was refused or could not be recorded; 2 refusal: usage error or fail-closed refusal |
| resultType | friction_result |
| agentVisible | true |
| executable | true |
| example | specd friction safe-create S1-01 --domain orchestration --blocked-operation next --consequence no-route-exists --revision 6 |
