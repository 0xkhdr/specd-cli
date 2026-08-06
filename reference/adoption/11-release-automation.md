# 11 — Release is a PR; tagging is automated

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P10](../patterns.md#p10--release-is-a-pr-tagging-is-automated-dispatch-is-retry-only) | 3 | medium | medium | not applied |

## Why

buzz's release lanes work like this: a local recipe opens a version-bump PR that
bumps manifests, regenerates lockfiles, and updates the changelog. **Merging the
PR is the human gate.** `auto-tag-on-release-pr-merge.yml` maps the branch prefix
to a tag prefix and pushes the tag with a dedicated GitHub App token, while "the
workflow's default `GITHUB_TOKEN` remains read-only and is never used to create a
tag." Manual dispatch "is only a retry mechanism for an existing immutable
`v<version>` tag. It cannot build from `main`."

specd already has the hard half: `release/tag-contract.sh` refuses a lightweight
tag or a version with no changelog section, and `release.yml` re-runs the full
four-platform gate against the tag before publishing. What is missing is that a
human types the tag by hand, which means the version bump, the changelog entry,
and the tag are three independently forgettable steps.

The philosophical fit is the point: specd is a harness whose thesis is that a
human authorizes a transition and a machine enforces it. Its own release should
be shaped that way — the reviewable artifact is the PR, and the tag is a
mechanical consequence of merging it.

## Change set

### 11.1 A release-preparation script

`release/prepare.sh`, POSIX `sh`, matching `tag-contract.sh`'s existing style:

```sh
#!/bin/sh
# Opens (or updates) a release/<version> branch carrying exactly the metadata a
# release needs: the CHANGELOG section for <version>, and any version constant
# the build reads. It creates no tag and publishes nothing. Merging the PR it
# produces is the human gate; auto-tag turns that merge into the tag.
#
# Refuses on: not on main, dirty tree, a version that already has a tag, or a
# version whose CHANGELOG section is missing or empty. Each refusal names one
# legal next action, matching how the harness itself refuses.
set -eu
```

Requirements:

- refuse unless the working tree is clean and the branch is the default branch;
- refuse if `v<version>` already exists locally or on the remote — a released
  version is immutable, the same rule buzz applies to mobile RC tags;
- **run `sh release/tag-contract.sh` against the prospective version** before
  opening anything, so the contract that will gate the tag is checked before the
  PR exists rather than after the merge;
- write or verify the `CHANGELOG.md` section;
- create branch `release/<version>`, commit, and stop. Opening the PR is the
  operator's action (`gh pr create`), and the script prints the exact command.

Add `make release-prep VERSION=x.y.z` in the `Makefile`.

### 11.2 Auto-tag on merge

`.github/workflows/auto-tag.yml`:

```yaml
name: auto-tag

# The tag is a mechanical consequence of merging a release PR, not a thing a
# human types. A mistyped tag is caught by release/tag-contract.sh at release
# time — after the gate matrix has already been paid for. Deriving the tag from
# the branch name makes the mistyped case unreachable.
on:
  pull_request:
    types: [closed]
    branches: [main]

permissions:
  contents: read

jobs:
  tag:
    if: >
      github.event.pull_request.merged == true &&
      startsWith(github.event.pull_request.head.ref, 'release/') &&
      github.event.pull_request.head.repo.full_name == github.repository
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions:
      contents: write   # the only job in the repository that may create a ref
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.pull_request.merge_commit_sha }}
          fetch-depth: 0
          persist-credentials: false
      # The contract runs again here, against the merge commit, before the tag
      # exists. A tag that could not pass the contract is never created, so a
      # failed release never leaves a tag behind to be deleted or moved.
      - name: tag contract
        run: |
          VERSION="${HEAD_REF#release/}"
          sh release/tag-contract.sh --self-check
          sh release/tag-contract.sh "v${VERSION}"
        env:
          HEAD_REF: ${{ github.event.pull_request.head.ref }}
      - name: create annotated tag
        run: |
          VERSION="${HEAD_REF#release/}"
          git config user.name  "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git tag -a "v${VERSION}" -m "v${VERSION}"
          git push origin "v${VERSION}"
        env:
          HEAD_REF: ${{ github.event.pull_request.head.ref }}
```

**One caveat to verify before relying on this**, and to record in
`release/release-decision.md` when it lands: a tag pushed with the default
`GITHUB_TOKEN` does **not** trigger another workflow. buzz solves this with a
dedicated GitHub App token precisely so `release.yml`'s `on.push.tags` fires.
Two acceptable resolutions for specd:

- **A dedicated App or fine-grained PAT** with contents:write, as buzz does. It
  is the same trust model already implied by the release job's `contents: write`.
- **Keep the release run manually dispatched** against the created tag. The tag
  becomes automatic and immutable; publishing stays a deliberate human action.

Given specd's posture — human approves, machine enforces — the second is the
better default. Choose it unless the operator wants unattended publishing, and
write the choice and its reason into `release/release-decision.md`.

### 11.3 Manual dispatch is retry-only

If `release.yml` gains a `workflow_dispatch`, constrain it the way buzz
constrains its own: it accepts only an existing `v*` tag ref and a matching
version input, and refuses a branch ref. State it in the workflow comment and in
`docs/contributing.md`. A dispatch that can build from `main` is a path to
publishing a binary whose version no tag backs — the exact defect that got
`v0.1.0` retracted.

### 11.4 Immutability

Add to `SECURITY.md` (or `release/release-decision.md`) the rule buzz repeats
throughout its troubleshooting: **a published tag is never moved or deleted.** A
bad release is superseded by a new version, and, where the defect is a false
claim rather than bad code, retracted in `go.mod` — which specd has already done
once and is the precedent to cite.

## Acceptance

Dry-run on a scratch fork:

```bash
sh release/prepare.sh 0.3.1        # creates release/0.3.1, no tag, prints the gh command
sh release/tag-contract.sh v0.3.1  # passes against the prepared changelog
```

Merge the PR on the fork; confirm `v0.3.1` appears as an **annotated** tag on the
merge commit, that `git tag -v`/`git cat-file -t` shows a tag object, and that no
tag is created when the contract fails (test by preparing a version with an empty
changelog section).

## Do not

- **Do not let any workflow move or delete a tag.** `git push --force` on a tag
  ref should appear nowhere in this repository.
- **Do not widen the default `GITHUB_TOKEN`.** Only the `tag` job gets
  `contents: write`, and only for the ref it creates.
- **Do not skip the contract in the auto-tag job** because the release workflow
  runs it later. Creating a tag that will fail the contract leaves a bad
  immutable ref behind, and the fix for that is another version — cheap to
  prevent, permanent to get wrong.
- **Do not automate the changelog body.** `CHANGELOG.md` is written by a human;
  the script verifies the section exists and is non-empty, which is the gate that
  matters.

## Deferred

Multiple release lanes. specd ships one artifact set from one tag; buzz's
three-lane structure exists because desktop, relay, and mobile version
independently. Adding lanes to a single-binary project would be scaffolding for
a need that has not appeared.
