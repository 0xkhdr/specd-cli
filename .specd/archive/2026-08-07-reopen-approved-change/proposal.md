# Proposal

## Problem
An approved change can become unusable when its authored plan is found to be
wrong or its active attempt can no longer be verified. Editing the plan makes
approval stale, but no operation returns the same change to planning. The
agent is left with an impossible next action or must duplicate the change and
lose its identity and recovery history.

## Outcome
An agent can revoke execution authority with one revision-checked `reopen`
operation. The existing change returns to planning, old records remain
append-only, task activity and attempt bindings are cleared, and fresh check
and human approval are required before execution resumes.

## Scope
The approved-to-planning recovery transition, its typed history record, CLI
registry and projection, refusal behavior, retained journey, ownership map,
and the documentation that owns lifecycle recovery.

## Non-goals
Reopening reconciling or archived changes; preserving old task completion as
current; approving repaired bytes; deleting attempts or evidence; changing
project files; resetting Git; adding force, cancel, abandon, or a general
lifecycle transition API; addressing unrelated feedback findings.

## Affected capabilities
lifecycle-recovery
