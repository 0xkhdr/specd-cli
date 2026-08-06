# Proposal

## Problem
The branch makes Windows device names invalid managed path segments on every
platform, but the naming and refusal documentation still describes only the
six managed-tree words as reserved. A reader can therefore choose a documented
valid name that the harness refuses.

## Outcome
The hand-written path documentation matches `path.ValidateSegment` and names
both reserved-name families without duplicating implementation detail.

## Scope
The naming contract in `docs/layout.md` and the `reserved_segment` recovery row
in `docs/troubleshooting.md`.

## Non-goals
Changing validation behavior, refusal codes, generated operation
documentation, or unrelated prose discovered during the branch audit.

## Affected capabilities
release-assurance
