# Surface inventory

Stage 9 subtraction audit. Every surviving surface of specd v2 maps here to
exactly one owner:

- `journey:NN` — the retained release journey that exercises it, numbered as in
  `requiredJourneys` (`internal/integration/release_test.go`) and replayed by
  `internal/integration/release_journeys_test.go`.
- `invariant:<name>` — one of the foundation invariants stage 9 forbids trading
  for surface reduction: validation, approval, authority, scope, evidence,
  staleness, atomicity, fail-closed.
- `contract:<name>` — a named external contract the surface must satisfy.

This document is data, never a second source of truth about what exists. The
live inventory is derived from the repository with `go/ast` plus the canonical
registries (operation registry, record codec, guidance template, host adapter)
by `internal/integration/subtraction_test.go`, which fails when a live surface
has no row here and when a row here describes surface that no longer exists.

Classes are the stage 9 inventory classes as they appear in Go source:
`package`, `type`, `interface`, `symbol` (exported function or value), `method`,
`field`, and `json field` (a persisted field of a durable document). Commands,
config keys, record kinds, generated instructions, and adapter hooks come from
their registries and are listed individually.

## Go package surface

| package | classes | owner |
| --- | --- | --- |
| `cmd/specd` | package | contract: Go program entry point; `main` is the only symbol a Go binary must expose, and the path is the installed binary's name |
| `internal/agentjson` | field, json field, method, package, symbol, type | journey:14 — the cold agent reads every fact through this one envelope |
| `internal/cli` | package, symbol | contract: the `specd` argv surface; it parses one invocation and routes it to dispatch |
| `internal/cmd` | field, json field, method, package, symbol, type | journey:01 — every operation of the base loop is dispatched here |
| `internal/context` | field, json field, method, package, symbol, type | journey:01 — the bounded read context of one task |
| `internal/core` | field, json field, method, package, symbol, type | journey:01 — lifecycle, approval, readiness, scope, evidence, and completion truth |
| `internal/core/evidence` | field, json field, method, package, symbol, type | journey:09 — failing and zero-match verification decide evidence applicability |
| `internal/core/failure` | field, method, package, symbol, type | invariant:fail-closed — one refusal shape carrying exactly one legal next action |
| `internal/core/gates` | field, method, package, symbol, type | invariant:validation — the planning and production gates over authored artifacts |
| `internal/core/lock` | package, symbol | invariant:atomicity — the root and change locks that serialize managed writes |
| `internal/core/path` | method, package, symbol, type | invariant:scope — the only resolver of managed paths and reserved segments |
| `internal/core/persist` | package, symbol, type | invariant:atomicity — old-or-new bytes for every managed write |
| `internal/core/record` | field, json field, method, package, symbol, type | journey:04 — interrupted append and replay of history and evidence |
| `internal/core/report` | field, json field, package, symbol, type | journey:13 — default and production report projections |
| `internal/core/state` | json field, method, package, symbol, type | journey:05 — corrupt and future state fails closed here |
| `internal/core/transaction` | field, json field, package, symbol, type | journey:10 — sync conflict and injected multi-file write failure |
| `internal/core/verify` | field, json field, method, package, symbol, type | journey:09 — bounded verification runs recorded at current HEAD |
| `internal/exec` | field, method, package, symbol, type | journey:09 — the one shell-free bounded process execution |
| `internal/generate` | field, package, symbol, type | journey:14 — the generated guidance a fresh agent resumes from |
| `internal/host` | json field, method, package, symbol, type | journey:12 — the human gate and honest host assurance at handoff |
| `internal/plan` | field, method, package, symbol, type | journey:02 — brownfield proposal, delta, design, and task parsing |
| `internal/reconcile` | field, json field, package, symbol, type | journey:02 — approved deltas becoming accepted specs |

## Registry surface

| class | item | owner |
| --- | --- | --- |
| command | init | journey:01 — fresh project adoption |
| command | new | journey:01 — change creation |
| command | check | journey:05 — malformed artifacts are refused here |
| command | approve | invariant:approval — human semantic authorization of the plan |
| command | reopen | journey:06 — stale approved bytes and a moved-HEAD attempt return to planning without losing identity or history |
| command | status | journey:14 — the cold agent's lifecycle and approval facts |
| command | next | journey:03 — the ready frontier across two waves |
| command | context | journey:01 — bounded context for one task |
| command | start | journey:08 — the attempt that binds declared scope and baseline |
| command | verify | journey:09 — current-HEAD evidence for passing and failing runs |
| command | complete | journey:01 — completion consuming applicable passing evidence |
| command | review | journey:13 — the separate reviewer verdict of the production profile |
| command | sync | journey:10 — reconciliation as one transaction |
| command | archive | journey:11 — archive and its target collision refusal |
| command | report | journey:13 — the four read-only projections |
| command | friction | journey:03 — the D14 observation of a task the frontier reports as stopped |
| config key | global --root | journey:01 — explicit root selection, never inferred |
| config key | global --json | journey:14 — the machine-readable projection a fresh agent reads |
| config key | new --capability | journey:02 — the capability a brownfield delta is authored under |
| config key | approve --approver | invariant:approval — the recorded human identity |
| config key | approve --reason | invariant:approval — the recorded reason for that authorization |
| config key | reopen --revision | invariant:staleness — the observed revision before execution authority is revoked |
| config key | reopen --reason | journey:06 — the machine-checked explanation for returning the same change to planning |
| config key | context --budget-bytes | journey:01 — the bound on assembled context |
| config key | start --revision | invariant:staleness — the observed revision an attempt is guarded by |
| config key | verify --timeout | journey:09 — wall-clock bound on a verification run |
| config key | verify --output-limit | journey:09 — byte bound on recorded output |
| config key | complete --revision | invariant:staleness — the observed revision completion is guarded by |
| config key | review --reviewer | journey:13 — the reviewer identity claim |
| config key | review --verdict | journey:13 — recording versus projecting a verdict |
| config key | review --findings | journey:13 — bounded findings required to reject |
| config key | sync --approver | invariant:approval — the human who authorized accepted truth |
| config key | sync --reason | invariant:approval — the reason recorded with that authorization |
| config key | report --kind | journey:13 — selection among the four canonical reports |
| config key | report --profile | journey:13 — default and production comparison |
| config key | friction --domain | journey:03 — the deferred domain an observation names |
| config key | friction --blocked-operation | journey:03 — the operation that was stopped |
| config key | friction --consequence | journey:03 — what the missing capability prevented |
| config key | friction --revision | invariant:staleness — the observed revision the observation is bound to |
| record kind | created | journey:01 — the initial revision transition of a change |
| record kind | approved | journey:06 — the approval an artifact byte change makes stale |
| record kind | task_transition | journey:03 — task activity moving across waves |
| record kind | attempt | journey:08 — declared scope and baseline bound to one attempt |
| record kind | completion | journey:01 — completion consuming one evidence identity |
| record kind | observed | journey:09 — a verification observation, never completion |
| record kind | synced | journey:10 — the accepted-truth mutation and its inputs |
| record kind | archived | journey:11 — a change becoming immutable local history |
| record kind | friction | contract: D14 friction threshold; two independent records plus a dated owner decision are the only route into a deferred domain |
| record kind | reopened | journey:06 — append-only revocation of an approved plan and its active attempt binding |
| generated instruction | Root and change | journey:14 — the selected root and current change |
| generated instruction | Who owns what | journey:14 — owners of state, evidence, and accepted behavior |
| generated instruction | The loop | journey:01 — the base loop the agent follows |
| generated instruction | Declared scope | journey:08 — the out-of-scope diff refusal |
| generated instruction | Verification is not completion | journey:09 — evidence never completes a task by itself |
| generated instruction | The human gate | journey:12 — handoff instead of self-approval |
| generated instruction | When you are refused | journey:05 — one legal next action after a refusal |
| generated instruction | Host assurance | journey:12 — advisory scope stated honestly |
| generated instruction | Review is a separate pair of eyes | journey:13 — the reviewer is neither approver nor implementer |
| generated instruction | Reports project truth, they do not change it | journey:13 — reports authorize nothing |
| generated instruction | Operations available to you | journey:14 — the agent palette derived from the registry |
| adapter hook | LocalCapabilities | journey:12 — the honest declared capability set of the local host |
| adapter hook | Local | journey:12 — construction of the one host adapter |
| adapter hook | Capabilities.Assurance | journey:12 — the assurance label a capability set earns |
| adapter hook | Adapter.Root | journey:12 — the root the adapter installs into |
| adapter hook | Adapter.Capabilities | journey:12 — the declared capabilities of that adapter |
| adapter hook | Adapter.Assurance | journey:12 — advisory unless the host proves more |
| adapter hook | Adapter.Callable | journey:12 — the operations this host may expose |
| adapter hook | Adapter.Install | journey:14 — installing the generated guidance surface |
| adapter hook | Adapter.Invoke | journey:12 — argv for one operation, with the human boundary enforced |

## Speculative patterns challenged

`internal/integration/subtraction_test.go` checks these mechanically; prose
cannot satisfy them.

| pattern | result |
| --- | --- |
| persistent `in_progress` | Absent. Activity has exactly five values — `pending`, `in_progress`, `completed`, `failed`, `blocked` — and `in_progress` exists only between one attempt and its completion. `failed` and `blocked` are reachable resting states with declared transitions out of them, not parking spaces for an abandoned attempt. |
| extra lifecycle phases | Absent. Five phases are declared and each is reachable by a declared operation. |
| one-implementation interfaces | Absent. No exported interface is declared anywhere in the module. |
| config for fixed values | Absent. There is no config file, schema, or key store: the whole configuration surface is the declared flags listed above. |
| duplicate parser/resolver/envelope/report | Absent. The `Envelope`, `Adapter`, `Delta`, and `Report` models are each declared by exactly one package. |
| second adapter abstraction | Absent. `internal/host` is the one adapter and declares no plugin or registry surface. |
| fields reserved for deferred domains | Absent, with one exact allowance: `internal/plan.Migration` is the D10 `**Migration**:` field of a REMOVED requirement, a parsed document contract rather than a migration domain. |

## Pending deletion

Each row is a release blocker: the item is exported, is referenced nowhere, and
therefore cannot be reached by any journey. This audit deletes nothing —
removal happens through a separate approved task with exactly these files in
its declared scope. None of the three removes a validation, approval,
authority, scope, evidence, staleness, atomicity, or fail-closed behavior,
because none of them is reached by one.

None. The three items this audit first raised — `internal/core/path.RecordLock`,
`internal/core/state.ErrStaleRevision`, and
`internal/core/transaction.RecoverUnderRootLock` — were removed by S9-SUB-02,
S9-SUB-03, and S9-SUB-04. No unreferenced exported surface remains.

| item | required task |
| --- | --- |
