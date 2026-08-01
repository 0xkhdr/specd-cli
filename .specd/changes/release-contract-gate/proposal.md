# Proposal

## Problem
CONTRIBUTING.md states what makes a release complete, and the release workflow
enforces one clause of it late and one of them not at all. A tag whose version
has no CHANGELOG.md section is refused, but only in the final publish step,
after four platforms have run the suite, five binaries have been built, and a
provenance attestation has been recorded. A lightweight tag is refused nowhere,
so a release could ship carrying no tagger, no date, and no message.

## Outcome
Both clauses are checked in seconds, before any gate, build, or attestation
runs. A tag that does not satisfy the release contract fails at the start of the
workflow with a message naming the clause it missed.

## Scope
The release workflow, and the records that state what the release machinery has
been through.

## Non-goals
Checking the remaining clauses of the release contract. Whether the gating
platforms are green is what the gates job already decides, and whether the
release page carries the changelog section is decided by the publish step that
writes it from that section. Neither needs a second owner.

## Affected capabilities
release
