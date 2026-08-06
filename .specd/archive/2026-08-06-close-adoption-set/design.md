# Design

## Boundaries
release-assurance/Requirement: Observed platform claims
release-assurance/Requirement: Actionable state refusal

The maturity gate in release qualification, the one reader of a change's state
file, and the records that describe them. The registry rows themselves, the
recorded observation dates, and every other gate stay unchanged.

## Interfaces
Release qualification parses the supported platform paragraph and the raced
suite row already present in the release decision, and compares both with the
typed registry it already reads. The state reader returns the refusal its one
caller previously constructed, so no caller gains a new error type.

## Invariants
A platform row is proven exactly when the release decision supports it, and
carries the date of the raced-suite observation recorded there. A missing or
unreadable state file produces one stable refusal code with exactly one legal
next action, and that action never names the operation that produced it. The
registry stays the only place a level is typed and the release decision stays
the only place it is authored.

## Failure behavior
An unsupported platform claimed proven, a supported platform claimed lower, or
a platform date that differs from the recorded observation turns the maturity
gate red and forbids the release decision. An unreadable state file refuses
before any read of the change's plan, and no state, history, or evidence byte
changes.

## Integration
Reuse the existing document-parsing gate style, the maturity claim projection,
the shared refusal constructor, and the existing check_state code, so no new
refusal code reaches an agent and no second validation boundary appears.

## Alternatives
Restating each platform's level in prose would satisfy the existing
sentence-matching check, but it would put one claim in a second authored place,
which the adoption set exists to prevent. Deriving the date from the filesystem
or the clock would date a claim by when the test ran rather than by when the
suite was observed.

## Owner
internal/integration
