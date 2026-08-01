# Proposal

## Problem
The project has good documentation, but its two entry points repeat the same
loop, command list, status caveat, and page index. A new reader has to decide
which page to read before understanding whether they are a user, agent
integrator, or contributor.

## Outcome
The root README is a concise product landing page with installation, a short
first-use path, guarantees, and clear links into the docs. `docs/README.md` is
the single documentation map, organized by reader goal and recommended order.
Existing detailed pages remain stable and link to one another by topic.

## Scope
Rewrite `README.md` and `docs/README.md` in plain, human-readable language;
remove duplicated reference material from those entry points; add audience
paths for users, agent integrators, and contributors; and link the release,
security, changelog, generated operation reference, and binding agent guide.

## Non-goals
Renaming or splitting the existing topic pages, changing product behavior,
editing generated `docs/operations.md`, or changing release claims.

## Affected capabilities
documentation
