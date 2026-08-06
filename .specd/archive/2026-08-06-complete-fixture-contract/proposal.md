# Proposal

## Problem
The release-fixture README requires adversarial fixtures named for every
foundation invariant, but the tree contains only the authored fresh-project
input. The existing bite tests exercise refusals without a committed fixture
index, so a missing invariant case or an unread fixture is invisible.

## Outcome
Committed release fixture cases enumerate every protected invariant, drive the
existing refusal paths, assert the exact refusal code and one legal next
action, and fail when a fixture is missing or unused.

## Scope
Integration-test fixtures, their coverage test, and the adoption and release
records describing this byte-contract boundary.

## Non-goals
Generating authored Markdown roots, duplicating production validation in a
fixture parser, or replacing the existing focused bite tests.

## Affected capabilities
release-assurance
