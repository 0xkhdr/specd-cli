## Purpose
Recover an approved change whose execution plan or attempt reached a dead end
without weakening approval, authority, evidence, scope, staleness, atomicity,
or append-only history.

## ADDED Requirements

### Requirement: Reopen an approved change
The system MUST provide one agent-callable, revision-checked operation that
returns an active approved change to planning without creating or copying a
change.

#### Scenario: Approved change returns to planning
- **WHEN** an agent reopens an approved change at its observed revision with a non-empty reason and the project worktree is clean
- **THEN** the same change moves to planning at the next revision and the result directs the author to repair the plan and run check

#### Scenario: Old execution state is revoked
- **WHEN** the approved change has task activity and an active attempt
- **THEN** all tasks project as pending, no attempt remains active, and execution requires check followed by fresh human approval

### Requirement: Refuse unsafe recovery
The system MUST fail closed without mutation when reopening is stale, targets
an unsupported lifecycle, lacks a reason or actor, or would abandon dirty
project work.

#### Scenario: Concurrent callers use one revision
- **WHEN** two callers reopen the same observed revision
- **THEN** exactly one succeeds and the other receives `stale_revision` with the current revision

#### Scenario: Dirty attempt work remains
- **WHEN** project files contain uncommitted work while an approved change is reopened
- **THEN** reopening is refused and the one next action is to make the project worktree clean before retrying

#### Scenario: Accepted truth cannot move backward
- **WHEN** reopening targets planning, reconciling, or archived lifecycle
- **THEN** it is refused with the one legal recovery for that lifecycle

### Requirement: Preserve append-only truth
The system MUST append one typed `reopened` record and retain every earlier
history and evidence record unchanged.

#### Scenario: Recovery records its inputs
- **WHEN** reopening succeeds
- **THEN** the record binds actor, reason, prior lifecycle, prior revision, prior approval identity, active attempt identity when present, and observed Git HEAD

#### Scenario: State replacement is interrupted
- **WHEN** the reopened record is appended but state replacement does not finish
- **THEN** retry completes the same transition and does not invent a second record

#### Scenario: Earlier evidence exists
- **WHEN** a change with earlier evidence is reopened
- **THEN** the evidence remains readable but is not applicable to the newly planned change
