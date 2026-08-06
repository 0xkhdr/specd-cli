# 14 — Contributor contract and extension cookbook

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P11](../patterns.md#p11--documentation-is-organized-by-failure-mode), [P12](../patterns.md#p12--every-rule-names-its-enforcement-and-forbids-the-escape-hatch) | 4 | small | low | not applied |

## Why

buzz's `CONTRIBUTING.md` does four things specd's does not, all of them cheap:

1. **States the commit convention and its enforcement.** Conventional Commits,
   required because the PR title becomes the squash-merge subject. DCO sign-off,
   enforced by a required check, with the repair procedure for a branch that
   already has unsigned commits.
2. **States what will not be merged, and why.** Four categories — large refactors
   without prior agreement, cosmetic churn, entirely new features with no
   discussion, drive-by changes bundled into an unrelated fix — followed by the
   reason: "not because they're bad ideas, but because we can't safely review
   them without prior discussion. That saves your time as much as ours."
3. **States the AI-assistance contract explicitly.** "AI-assisted PRs are
   welcome. No need to disclose the tools you used, but you own and must have
   reviewed the final code. Submissions that are clearly unreviewed may be
   closed with a pointer here."
4. **Ships a cookbook.** "How to Add a New Event Kind" is nine numbered steps,
   each naming the exact file and function. Same for MCP tools and HTTP
   endpoints.

specd's `CONTRIBUTING.md` is 2.5KB and `docs/contributing.md` covers build and
test. Neither states a commit convention, a merge policy, or the steps to add an
operation — and adding an operation is the single most likely change anyone will
make to this repository, touching the registry, the generated docs, the
generated agent guidance, the surface inventory, and possibly a journey.

Item 3 deserves emphasis for this project specifically. specd is a harness for
agent-authored changes. It should state its own position on agent-authored
contributions, and it is the one repository where that statement is not
boilerplate.

## Change set

### 14.1 Commit and PR contract

Add to `CONTRIBUTING.md`:

- **Commit convention.** Adopt Conventional Commits (`feat`, `fix`, `docs`,
  `refactor`, `test`, `chore`) *if* merges are squashed, since the PR title
  becomes the subject. If they are not squashed, say so and state the actual
  convention instead. Do not adopt a convention with no enforcement and no
  consumer — that is a rule that decays.
- **Duplicate search.** "Before opening: search open issues and PRs; link the
  closest one, or say 'none found'." Add the line to
  `.github/pull_request_template.md`, which is where it will actually be read.
- **Issue-first for anything beyond a small fix**, with the reason.
- **What will not be merged**, adapted to specd's actual refusals:
  - a new runtime dependency (it fails the release gate);
  - new exported surface with no owner row (it fails the build);
  - a second output surface, or a command-entry guard that restates a `core`
    refusal;
  - a bypass for evidence, approval, authority, scope, or validation;
  - a weakened bite test or a refreshed golden fixture used to make an unrelated
    change pass;
  - speculative abstraction — "complexity must earn its place through real use"
    is already in `AGENTS.md`; restate it here for the contributor audience.
- **AI-assisted contributions.** State the position. Suggested, in specd's own
  register: *specd exists to make agent-authored changes reviewable, so
  agent-authored contributions are welcome and need no disclosure. The contract
  is the same one the harness enforces: you are the human gate on your own PR.
  A change you have not read is a change nobody has approved.*

### 14.2 DCO — decide, then enforce or drop

buzz requires DCO sign-off with a required check. specd should either:

- **adopt it** — add the required check, document `git commit -s`, document the
  repair (`git rebase --signoff main`, force-push), and add the idempotent
  `commit-msg` hook to `.githooks/` (deferred out of
  [adoption 02](02-git-hooks.md) exactly for this decision); or
- **not adopt it** — and say nothing about it.

Do not document a sign-off convention without a check. buzz notes DCO is "the
most common reason new PRs stall"; an unenforced convention has the stall
without the guarantee.

### 14.3 The cookbook

Add to `docs/contributing.md` — numbered, each step naming the exact file:

**How to add an operation** (approximately):

1. Add the entry to the operation registry in `internal/core` — name, usage,
   effect class, example, flags.
2. Implement the operation in `internal/core`; it is the only place the decision
   is made and the only place the input is refused.
3. Route it in `internal/cmd` dispatch. Do **not** restate a `core` validation
   here — one validation boundary.
4. Project the result through the one envelope. No per-operation `RenderX`
   helper, even for a test — one output owner.
5. Regenerate: `SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run
   TestOperationProjectionParity`. Commit the regenerated `docs/operations.md`
   and the managed `AGENTS.md` block in the same commit.
6. Add owner rows to `release/surface-inventory.md` for every new exported
   symbol, naming a journey, invariant, or contract.
7. Add a bite test for every refusal the operation can produce, asserting the
   refusal code and its one legal next action.
8. Add or extend a journey if the operation is part of the base loop; if the
   fourteen required journeys change, `requiredJourneys` and
   `release/release-decision.md` change in the same commit.
9. Run `make ci`.

**How to add a gate**:

1. Implement it in `internal/integration/release_test.go` from repository facts.
2. Add its row to `release/release-decision.md`.
3. Add its limits section to `release/gate-limits.md` — what a green run does
   **not** establish.
4. Prove it bites: mutate the thing it protects, confirm it fails, revert,
   record what you tried.

**How to add a refusal**: which failure type, why the code is stable surface,
why exactly one next action, and where the message text lives (presentation is
`internal/cmd/output.go`, never core).

### 14.4 A contributor gotchas section

buzz's most-used section is `AGENTS.md` §Common Gotchas — numbered, symptom →
cause → fix, with the sentence explaining why the trap is expensive. Seed
specd's with the traps that actually exist here:

- forgetting to regenerate `docs/operations.md` after a registry change — the
  parity gate fails with a byte diff, which reads like a rendering bug rather
  than a missed command;
- editing the managed block of `AGENTS.md` by hand — refresh refuses it as
  drift, by design;
- adding an exported symbol without an owner row — the build fails in
  `subtraction_test.go`, in a package the author never touched;
- introducing a noun outside the vocabulary table — the dead-vocabulary gate
  fails on generated output, not on the source line that caused it;
- hand-editing anything under `.specd/` — never a repair, always a refusal you
  caused;
- CRLF on Windows checkouts — pinned by `.gitattributes`; a checkout that
  bypasses it drifts every golden fixture at once, which looks like mass test
  failure rather than a line-ending problem.

That last one is the shape to aim for: the trap, plus what it *looks like* when
it happens.

## Acceptance

- `CONTRIBUTING.md` states the commit convention, the merge policy, and the AI
  position, and each rule names its enforcement.
- `docs/contributing.md` contains the three cookbooks; a contributor can add an
  operation from them alone.
- The DCO decision is made and recorded either way.
- The relative-link release gate passes over every new cross-link.

## Do not

- **Do not document a rule with no enforcement.** Either add the check or drop
  the rule. Every existing rule in `AGENTS.md` names its gate; a new one that
  does not will be the first to be ignored.
- **Do not duplicate `AGENTS.md`.** `AGENTS.md` is binding and terse;
  `CONTRIBUTING.md` is the friendly path into it. Where they overlap,
  `CONTRIBUTING.md` links.
- **Do not copy buzz's process wholesale.** Squash-merge policy, DCO,
  CLA language, and a code-of-conduct contact address are governance decisions
  for this project's maintainer, not defaults to inherit.

## Deferred

A `GOVERNANCE.md`. buzz's is one line. specd is maintained by one person and
`SECURITY.md` already states that plainly, which is more useful than a
governance document describing a body that does not exist.
