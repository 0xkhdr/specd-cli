# Design

## Boundaries
documentation/Audience-oriented entry points
Only the two hand-written entry points, `README.md` and `docs/README.md`, are
changed. Existing topic pages remain the detailed source for their subjects.

documentation/Single ownership of documentation navigation
`docs/README.md` owns the reading map; the root README links into it instead of
repeating its full page index.

documentation/Existing claims remain linked and verifiable
Links point to existing repository documents and generated references; no
behavioral or release claim is rewritten.

## Interfaces
The Markdown files are the user-facing documentation interface. Navigation uses
relative Markdown links that the existing release test resolves from each file.

## Invariants
`docs/operations.md` remains generated and byte-checked. The root README stays
short enough to orient a new reader; detailed command facts live in the
generated reference. Every audience path names a next page.

## Failure behavior
Broken links or stale generated documentation fail the existing release
qualification test. If a topic is not covered by an existing page, the index
links to the closest authoritative repository document rather than inventing a
new page.

## Integration
The root README remains the public landing page. `docs/README.md` becomes its
documentation hub and routes to `getting-started.md`, `concepts.md`,
`the-loop.md`, `agent-setup.md`, `troubleshooting.md`, `layout.md`,
`approval-and-evidence.md`, `operations.md`, and `contributing.md`, plus the
root-level release and policy documents.

## Alternatives
Adding subdirectories or renaming the nine existing topic pages would create
link churn without adding information. A new overview page would duplicate
the existing docs index, so the current `docs/README.md` remains the owner.

## Owner
repository documentation
