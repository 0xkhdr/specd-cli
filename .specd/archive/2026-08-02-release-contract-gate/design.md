# Design

## Boundaries
release/Requirement: Tag contract precedes release work
One job in `.github/workflows/release.yml`, running on the pushed tag. It reads
the tag object and `CHANGELOG.md` and decides one thing: whether this tag is
allowed to consume a gate run. It does not build, publish, or write anything.

## Interfaces
`release/tag-contract.sh`, taking the tag name as its one argument, decides the
question and is the only thing that knows the two clauses. It also accepts
`--self-check`, which runs the clauses against fixtures it builds and removes
itself. A `contract` job with no dependencies runs it, and
`gates` then depends on that job, so the existing `gates` -> `publish` order
becomes `contract` -> `gates` -> `publish`. The script's inputs are the tag name
and the checked-out tree; its output is its exit status and one line per clause
it refuses.

The check lives in a script rather than inline YAML because the workflow is the
one part of this repository that cannot be run locally. A script can be, which
is what lets this change verify anything at all before a tag is pushed.

## Invariants
The tag object is annotated: `git cat-file -t "$GITHUB_REF_NAME"` reports `tag`,
not `commit`. `CHANGELOG.md` has a heading for the version the tag names. The
checkout must fetch tags for the first check to see a tag object at all.

## Failure behavior
Fails closed and names the clause missed. A lightweight tag and a missing
changelog section are reported separately, because they are fixed differently:
one is retagged, the other is written. Nothing downstream runs, so a refused tag
costs seconds and leaves no release, no assets, and no attestation.

## Integration
The publish step keeps its own `awk` extraction of the changelog section and the
emptiness check that follows it. That extraction is the single owner of parsing
the section, and it stays where its output is used. The contract job asks a
different and cheaper question — whether a heading for this version exists at
all — so the two are not two parsers of one contract.

Pushing a tag is still the only thing that exercises the job wiring. The
task-level verification runs the script's own self-check, which covers the
clauses and their refusals; it does not prove the YAML, and this design does not
claim it does.

## Alternatives
Extracting the section once in `contract` and passing it to `publish` as an
artifact would remove the second look at the file, but it adds an upload and a
download to carry a few lines between jobs, and it moves the notes away from the
step that publishes them. Rejected as more machinery than the duplication costs.

Checking the annotated tag inside the existing publish step was rejected because
it would refuse after the expense the check exists to avoid.

## Owner
.github/workflows/release.yml
