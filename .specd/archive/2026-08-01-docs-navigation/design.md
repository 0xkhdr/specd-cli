# Design

## Boundaries
documentation/Requirement: Audience-oriented navigation
`README.md` owns product orientation and the shortest evaluation path;
`docs/README.md` owns audience routing and reading order. Topic pages own their
subject and link outward instead of repeating the full loop or command palette.

documentation/Requirement: Consistent developer-friendly voice
All hand-written documentation uses the nouns defined in `AGENTS.md`, direct
second-person instructions where action is required, short introductions,
descriptive headings, and concise examples. Existing pages are revised in
place; no new hierarchy or documentation tooling is added.

documentation/Requirement: Current operational guidance
The operation registry and generated `docs/operations.md` own command syntax,
flags, lifecycles, and exits. Source and tests own behavior. Hand-written pages
explain workflows and link to those owners rather than maintaining competing
reference tables. Walkthrough output is checked against the current binary.

documentation/Requirement: Honest production boundary
`release/release-decision.md` owns proven and unproven release claims;
`SECURITY.md` owns security boundaries; `release/surface-inventory.md` owns
surface ownership. User guides summarize only what their audience needs and
link to these files for the authoritative boundary.

## Interfaces
The interface is the existing Markdown set: `README.md`, the nine hand-written
pages under `docs/`, and generated `docs/operations.md`. Relative links remain
portable on GitHub and in a repository checkout. No site generator or external
documentation dependency is introduced.

## Invariants
`docs/operations.md` remains generated and byte-checked. Managed `.specd/`
state remains untouched. Project vocabulary stays canonical. No page invents
an operation, bypass, platform guarantee, host containment claim, deployment
effect, or production assurance. Each page has one audience, one subject, and
a useful next link.

## Failure behavior
Broken links, generated-document drift, dead vocabulary, or release-surface
drift fail existing tests. A prose claim that cannot be matched to current
source, tests, security policy, or release evidence is removed or qualified.
An example that cannot be reproduced is corrected from current output rather
than preserved for narrative continuity.

## Integration
Entry points route into topic pages. Topic pages link to
`docs/operations.md` for exact command contracts, `docs/troubleshooting.md` for
recovery, and release or security documents for assurance boundaries.
`docs/contributing.md` records the maintenance rules needed to keep this
ownership model current.

## Alternatives
Renaming, splitting, or nesting the existing pages creates link churn without
fixing voice or accuracy. Adding a site generator adds maintenance and a
dependency while Markdown already serves the repository audience. Rewriting
generated operation docs by hand would create a second source of truth.

## Owner
repository documentation
