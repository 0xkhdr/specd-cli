# Design

## Boundaries
release-assurance/Requirement: Managed-name documentation parity
Only the two hand-written pages that describe managed names and their refusal
recovery change.

## Interfaces
Existing Markdown naming paragraph and troubleshooting table row.

## Invariants
Source remains authoritative. `docs/operations.md` remains generated and
untouched. The stable refusal code and legal next action do not change.

## Failure behavior
If the wording or links violate repository contracts, release qualification
fails and no documentation update is accepted.

## Integration
`path.ValidateSegment` and its tests are the behavioral owner;
`TestReleaseQualification` checks the hand-written documentation set.

## Alternatives
Listing every Windows device spelling is unnecessary; the two compact families
and representative examples describe the existing finite rule.

## Owner
docs/layout.md and docs/troubleshooting.md
