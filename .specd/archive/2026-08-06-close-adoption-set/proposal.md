# Proposal

## Problem
Two of the fourteen applied adoption items were marked applied without their
acceptance being run, and running it found defects. The typed maturity registry
does not bite: upgrading a gated platform to proven — a published claim with no
observation behind it — leaves the whole suite green, because only four claim
ids are cross-checked against prose and a claim's date is checked for shape
alone. Separately, every operation that loads a change's state except check
leaks a raw filesystem error when state.json is missing, and offers as recovery
the command that just failed.

## Outcome
A platform claim cannot be upgraded or re-dated without the release decision
that earns it, and every operation that loads state refuses a missing or
unreadable state file with one stable code and one legal next action. The
adoption records state what was observed rather than that the item was applied.

## Scope
The maturity gate in release qualification, the single reader of a change's
state file, their bite tests, and the release, troubleshooting, and adoption
records describing what those gates now establish.

## Non-goals
Re-dating the recorded platform observations, which requires reading a green
four-platform CI run; cutting a release through the release-PR path; and
gating profile, guarantee, or coverage levels, which have no second authored
place to disagree with.

## Affected capabilities
release-assurance
