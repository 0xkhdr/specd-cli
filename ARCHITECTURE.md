# specd architecture

This document explains how the shipped code is shaped. Product behavior belongs
in [`docs/`](docs/README.md), ownership belongs in
[`release/surface-inventory.md`](release/surface-inventory.md), and evidence
belongs in [`release/release-decision.md`](release/release-decision.md).

## 1. Summary

specd is one process: no daemon, network service, or model call. The CLI parses
one invocation, dispatches one operation, the core makes the decision under a
root or change lock, and one envelope renders as text or JSON.

```text
argv
  ↓
internal/cli
  ↓
internal/cmd ── dispatch and the one output owner
  ↓
internal/core ─ lifecycle, approval, readiness, scope, evidence, completion
  ↓
state · record · persist · transaction · filesystem · Git
```

Dependencies point toward the decision owner. `internal/core` never imports
`internal/cmd`; `gateDeterministicCore` in
`internal/integration/release_test.go` also refuses network imports in the
deterministic packages.

## 2. Layers

`cmd/specd` passes process arguments and streams to `internal/cli`. It contains
no behavior. `internal/cli` parses the registered command shape, derives the
actor route, and calls `internal/cmd`.

`internal/cmd` binds every executable operation to one handler. Handlers adapt
arguments and project results; they do not repeat a decision already made by
`internal/core`. `TestOperationDispatch` and `TestBaseLoopOperationsAreReachable`
fail when registration and dispatch disagree.

`internal/core` owns lifecycle transitions and refusals. Supporting packages
parse plans, assemble context, reconcile accepted specs, execute verification,
and project reports, but no outer layer may weaken a core refusal.

Durability packages sit below those decisions. They lock, validate paths,
replace files atomically, append records, and commit multi-file changes. Git and
the filesystem are inputs to that layer, not alternate sources of lifecycle
truth.

## 3. One-owner rules

### One output owner

All outcomes become one `agentjson.Envelope` and pass through
`cmd.RenderJSON` or `cmd.RenderText`. A per-operation renderer would create a
second surface. `TestSurfaceOwnership`, `TestAgentJSONGolden`, and
`TestReportsHumanAndJSONAgree` enforce ownership and text/JSON parity.

### One validation boundary

The core refuses invalid state or intent. Command handlers translate arguments
once and do not shadow those checks. Refusal tests such as
`TestApproveAgentCapableRouteRefusesBeforeFilesystem`,
`TestScopeRejectsOutsideDeleteRenameSymlinkAndManaged`, and
`TestCompleteRequiresCurrentProofAndExactScope` exercise that boundary.

### One parser per contract

Operation metadata lives in `internal/core/operations.go`; task and delta
syntax live in `internal/plan`; managed path resolution lives in
`internal/core/path`; the envelope lives in `internal/agentjson`; reports live
in `internal/core/report`. `TestSurfaceOwnership` rejects a second owner for
the named parser, envelope, report, and adapter roles.

### One claim registry

`internal/core/maturity.go` owns published platform, profile, guarantee, and
coverage levels. Generated guidance and `report --kind status` project the
same rows. `TestMaturityGateBites` proves missing evidence and contradictory
primary documentation make release qualification fail.

A new refusal therefore belongs in the core operation that decides it. It must
not be restated in `internal/cmd`, and its presentation belongs in the shared
output projection.

## 4. Operation registry

Each operation entry declares its id, summary, actor, effect, lifecycle,
arguments, flags, exits, result type, visibility, executability, and example.
That one registry projects:

- `specd --help`;
- [`docs/operations.md`](docs/operations.md);
- the managed agent guidance operation palette;
- dispatch and agent-contract reachability checks.

`TestOperationRegistry` validates entries.
`TestOperationProjectionParity` byte-checks generated operation documentation.
`gateGuidanceParity` checks deterministic guidance and callable visibility.
`TestOperationDispatch` checks that every executable id has exactly one handler.

Changing the registry changes its version and makes earlier plan approval
stale. That cost is deliberate and is enforced by
`TestApprovalStalenessRegistryPolicyRevision`.

## 5. Durability

A root lock protects root-wide records and a change lock serializes transitions
for one change. Locks carry no content. `TestManagedPathLockSerializes` and
`TestConcurrentCallersOneRoot` exercise the file-lock path across callers.

State writes use atomic replacement: write a temporary file, sync it where the
platform supports the operation, then rename it over the target.
`TestAtomicReplace` checks old-or-new bytes.

Every transition carries an expected revision. A mismatch refuses instead of
merging guesses. `TestMutateConcurrentRevision`,
`TestTaskTransitionConcurrentRevisionOneWinner`, and
`TestCompleteConcurrentCASOneWinner` prove one winner under contention.

History and evidence are append-and-replay ledgers. Records bind identities,
revisions, and hashes; replay rejects malformed, duplicate, future, or
non-contiguous input. `TestAppendConcurrentDuplicate`,
`TestReplayRefusesNonContiguousRevisionChain`, and
`TestEvidenceMalformedFutureDuplicateAndAmbiguousFailClosed` enforce this.

Multi-file sync uses a transaction record and deterministic recovery.
`TestCommitWritesEveryOutputOnce` and
`TestRecoverRollsForwardCommittedTransaction` prove recovery completes the
bound write set rather than inventing another one.

## 6. Trust model

The human/agent route is derived at the CLI boundary from a terminal check on
stdin. Agent-routed invocations cannot call human operations.
`TestDerivedRouteNeedsARealTerminal`, `TestHumanGateRoutes`, and
`TestSyncIsNeverHandedToAnAgent` enforce this separation.

Approval binds the exact authored artifact set, bytes, registry version, and
policy digest. `TestApprovalIdentityStableAndBindsPathBytes` and the journey 06
stale-approval case enforce that binding.

An attempt binds one task, declared file list, approval hash, state revision,
and Git HEAD. Scope validation includes ignored and untracked paths.
`TestAttemptCleanFrontierPersistsExactBinding`,
`TestScopeIncludesIgnoredUntrackedPaths`, and journey 08 enforce it.

Verification produces an observation. Applicability requires the same change,
task, attempt, command, approval, revision, and current HEAD. Completion alone
consumes that evidence. `TestEvidenceAppendAndStrictApplicability` and
`TestCompleteConsumesExactEvidenceOnce` enforce the split.

The harness validates scope but cannot prevent a process from writing outside
it. Host assurance remains advisory unless the host supplies containment. See
[`SECURITY.md`](SECURITY.md); `TestAgentContract` keeps that label on the
agent-facing surface.

## 7. Package map

The map explains shape; the [surface inventory](release/surface-inventory.md)
records ownership and is checked against live declarations.

| package | responsibility |
| --- | --- |
| `cmd/specd` | Process entry point. |
| `internal/agentjson` | One bounded machine-readable envelope. |
| `internal/cli` | Argument parsing, route derivation, and process exit. |
| `internal/cmd` | Dispatch and the one text/JSON projection owner. |
| `internal/context` | Bounded task read context and authority manifest. |
| `internal/core` | Lifecycle, approval, readiness, scope, evidence, completion, policy, and claims. |
| `internal/core/evidence` | Evidence and reviewer-verdict applicability. |
| `internal/core/failure` | Stable refusal codes with one legal next action. |
| `internal/core/gates` | Planning and production validation gates. |
| `internal/core/lock` | Root and change mutual exclusion. |
| `internal/core/path` | Canonical managed-path resolution. |
| `internal/core/persist` | Atomic single-file replacement. |
| `internal/core/record` | History and evidence record codecs and replay. |
| `internal/core/report` | Canonical read-only report models. |
| `internal/core/state` | State document validation and revision mutation. |
| `internal/core/transaction` | Atomic multi-file commit and recovery. |
| `internal/core/verify` | Verification result validation. |
| `internal/exec` | Bounded shell-free subprocess execution. |
| `internal/generate` | Agent guidance projection and managed-region refresh. |
| `internal/host` | Local host capabilities and invocation adapter. |
| `internal/integration` | Journeys, conformance, release, and ownership gates. |
| `internal/plan` | Proposal, design, task, and delta parsing. |
| `internal/reconcile` | Approved deltas into accepted specs. |

`TestSurfaceOwnership` fails when live exported surface lacks an inventory
owner or an inventory row describes removed surface.

## 8. Known limitations

- Host assurance is advisory; specd supplies no process sandbox. The maturity
  registry and `TestMaturityGateBites` prevent the primary summaries from
  claiming otherwise.
- Scale observations stop at the workloads in
  [`release/scale.md`](release/scale.md). Benchmark execution checks
  panic-freedom, not a supported limit.
- Two callers have not driven the entire loop concurrently. The registry marks
  that coverage unclaimed.
- macOS, Windows, and linux/arm64 replay the gated suite, but no real change has
  been hand-driven there. linux/amd64 is the only proven platform.
- Release gates observe a checkout, not a deployment. Their exact blind spots
  are in [`release/gate-limits.md`](release/gate-limits.md).

## 9. Decisions and reversal conditions

**Standard library only.** One static binary keeps installation and the
deterministic boundary inspectable. `gateStdlibOnly` enforces an empty require
set. Revisit only when a concrete requirement cannot be met correctly with the
standard library and the release owner accepts the new supply-chain boundary.

**Markdown and Git are user-visible truth.** They keep plans reviewable without
a server and make evidence identities reproducible. Planning parser tests and
the fourteen release journeys enforce the current contract. Revisit only when
a demonstrated requirement cannot be represented without losing a guarantee.

**One binary and no daemon.** Local operations need no service lifecycle or
remote authority. `gateDeterministicCore` and the package inventory enforce the
current shape. Revisit only through the deferred-domain threshold recorded in
the release decision.

**Explicit completion.** Verification is evidence, never completion.
`TestVerifyRefusalNeverCompletesOrWritesEvidence` and
`TestCompleteConsumesExactEvidenceOnce` enforce the split. Reverse it only if a
new requirement preserves independent evidence applicability and harness-owned
state transitions; convenience alone is insufficient.
