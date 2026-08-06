# Design

## Boundaries
release-assurance/Requirement: Adversarial fixture coverage

Test-only release fixtures and integration checks. Production command, state,
and output owners remain unchanged.

## Interfaces
One small typed case document per named fixture and one integration test that
discovers the fixture tree, drives the corresponding existing refusal setup,
and compares the observed refusal code and next action.

## Invariants
Every protected invariant has at least one case; every committed case is read;
each observed refusal has the declared code and exactly one legal next action.
The existing fresh-project authored input remains in place.

## Failure behavior
Missing, duplicate, malformed, or unread fixtures fail release qualification.
An unexpected success, refusal code, or next-action shape fails the named case.

## Integration
Reuse the release journey CLI driver, refusal envelope helpers, protected
invariant list, and release qualification suite. Documentation records that
authored inputs have no refresh switch.

## Alternatives
Renaming the existing fresh-project tree adds deletion and migration noise
without improving coverage. Generated full `.specd` roots would duplicate the
harness and blur authored input with expected output.

## Owner
internal/integration
