# buzz — complete analysis

Source: `buzz-reference/` in this repository, a checkout of `block/buzz`.
Every claim below cites the file it came from so a reader can re-derive it.

## 1. What buzz is

An agent-first collaboration platform built on Nostr. A Rust workspace of 26
crates plus three client surfaces (Tauri/React desktop, Flutter mobile, a
browser web client), deployed as a WebSocket relay backed by Postgres, Redis,
and S3-compatible media storage.

Its self-description in `CONTRIBUTING.md` reduces to two design axioms:

- **The relay is the single source of truth.** Crates communicate through the
  database and Redis pub/sub, not through direct cross-crate calls. The only
  shared code is `buzz-core` types.
- **Event kinds are the only switch.** Every action — a message, a reaction, a
  workflow step — is a Nostr event with a kind integer registered in one file
  (`crates/buzz-core/src/kind.rs`). Adding a feature means adding a kind, which
  is why the system extends without breaking existing clients.

This matters to specd because both projects made the same core bet: **one
canonical registry drives behavior, documentation, and validation.** specd's
operation registry is structurally the same idea as buzz's kind registry.

## 2. Repository shape

```
crates/           26 Rust crates, grouped by role in AGENTS.md
desktop/          Tauri 2 + React 19 + Vite + Tailwind
web/              browser client served by the relay
mobile/           Flutter + Riverpod + hooks
migrations/       SQL, applied on relay startup
docs/             operational docs, NIP specs, formal specs
docs/spec/        TLA+ (.tla/.cfg) and Tamarin (.spthy) models
docs/formal/      formal-method notes and Python model programs
benchmarks/       load harness
perf/             scaling models with their own tests
scripts/          ~40 dev/release/verification scripts
bin/              Hermit-pinned toolchain shims
schema/           schema.sql
deploy/           helm charts + compose
examples/         runnable example agents
```

Root-level governance files: `AGENTS.md` (594 lines), `ARCHITECTURE.md` (827),
`CONTRIBUTING.md` (477), `TESTING.md` (311), `RELEASING.md` (272), `SECURITY.md`,
`GOVERNANCE.md`, `CODE_OF_CONDUCT.md`, `NOSTR.md`, and seven `VISION*.md` files.
`CLAUDE.md` is a **symlink to `AGENTS.md`** — one agent guide, many agent
harnesses reading it.

## 3. The agent contract (`AGENTS.md`)

This is buzz's most directly transferable artifact. Its structure:

1. **Ecosystem** — the five repos, what each produces, and an ASCII dependency
   graph. An agent that does not know where the build actually happens will
   propose changes in the wrong repo.
2. **Repo structure** — an annotated tree, one line per crate.
3. **Getting started** — five commands, in order.
4. **Quality gates** — what to run, and specifically *why running one is not
   running the other* ("Clippy passing does not mean fmt passes; run both").
5. **Key patterns** — the architectural defaults, each phrased as a decision
   rule an agent can apply without reading source ("prefer a new event kind over
   a new HTTP endpoint").
6. **Per-surface rules** — desktop, mobile, CLI, each with hard prohibitions
   ("NEVER use `StatefulWidget`", "NEVER run `flutter build`", "no `unsafe`",
   "no new `unwrap()` in production paths").
7. **Common Gotchas** — seven numbered traps, each written as *symptom →
   cause → fix*, several with the sentence explaining why the trap costs real
   time ("That looks exactly like a product bug rather than a build mistake, so
   it burns real time").
8. **See Also** — the doc map.

Three properties make it work:

- **It documents failure modes, not features.** The largest sections are about
  what goes wrong and what it looks like when it does.
- **Every rule names its enforcement.** Not "keep files small" but "hard ceiling
  1000 lines/file, enforced by `mobile/scripts/check-file-sizes.mjs` via
  `just mobile-check`", followed by "**If the guard trips, split the file —
  never bump the limit or add an override to slip under it.**"
- **It is honest about deliberate deviations.** `lefthook.yml` opens with a
  comment block listing four places where the hook config intentionally diverges
  from CI, each with a reason, ending "Deliberate — accepted, not worked around."

## 4. Quality gates

### Local

One task runner (`Justfile`, 41KB, ~90 recipes) is the entire interface:

| Recipe | Role |
| --- | --- |
| `just setup` | bootstrap toolchain + Docker + migrations + hooks |
| `just check` | fmt-check, clippy, desktop-check, tauri fmt/clippy, web, mobile |
| `just fix-all` | auto-fix every formatter in one shot |
| `just ci` | `check` + unit tests + desktop build + tauri check/test + web build + mobile test |
| `just test-unit` / `just test` | no-infra tests / full integration with Docker |
| `just reset` | wipe and recreate dev state |

The invariant: **one command reproduces CI locally.** `CONTRIBUTING.md` states
"This is the same check that runs in CI. PRs that fail `just ci` will not be
merged."

### Git hooks (`lefthook.yml`)

- `pre-commit`, parallel, **auto-fixing**: each formatter runs with
  `stage_fixed: true`, so fixable problems are fixed and re-staged rather than
  rejected. Unfixable lint blocks the commit.
- `commit-msg`: appends the DCO `Signed-off-by` trailer idempotently via
  `git interpret-trailers --if-exists doNothing`.
- `pre-push`, parallel: branch-skew check plus the fast unit suites. Explicitly
  **no overlap with pre-commit**, and builds are CI-only.
- Every hook has a `glob:` matching CI's `dorny/paths-filter` groups, and the
  file's header comment commits to keeping the two in sync.

The design rule: **cheap and auto-fixing at commit, fast and verifying at push,
expensive only in CI.**

### Toolchain pinning

Hermit (`bin/activate-hermit`, `bin/hermit.hcl`) pins Rust, Node, pnpm, just,
lefthook, dart, flutter, biome, cargo-deny, cmake. CI activates the identical
environment with `cashapp/activate-hermit`, so local and CI run byte-identical
tool versions. `AGENTS.md` explicitly instructs agents not to work around an
unconfigured `PATH`: "do not rewrite hook commands to compensate."

## 5. CI (`.github/workflows/ci.yml`)

Fourteen jobs. The load-bearing structure:

- **`changes` job first.** `dorny/paths-filter` computes five booleans
  (`rust`, `desktop`, `desktop-rust`, `web`, `mobile`); every downstream job is
  `if: github.event_name == 'push' || needs.changes.outputs.<x> == 'true'`. PRs
  run only what they touched; pushes to `main` run everything.
- **Contract scripts run unconditionally in `changes`** — `test-release-ref-contract.sh`,
  `test-mobile-release-contract.sh`, `test-mobile-release-candidate-publisher.sh`,
  `test-mobile-worktree-overrides.sh`. Release machinery is tested like code.
- **`timeout-minutes` on every job** (2 to 45).
- **`permissions: contents: read`** at the workflow level and restated per job.
- **Every action pinned to a commit SHA** with a `# vN` trailing comment.
- **`concurrency` with `cancel-in-progress` only for pull requests**, so `main`
  builds are never cancelled.
- **A dedicated `security` job** running `cargo-deny check` (advisories,
  licenses, bans, sources).
- **A `dead-token-guard` job**: a grep-based regression fence that fails if
  removed API-token identifiers reappear in client code. A deletion is enforced
  by CI, not by memory.
- Cross-compile and per-OS canary jobs (`linux-canary`, `windows-canary`,
  `signed-macos-canary`) so a platform claim is backed by a run.

## 6. Supply chain (`deny.toml`, `renovate.json`)

`deny.toml` is a full dependency policy:

- **`[advisories].ignore`** — each ignored RUSTSEC ID carries a written reason,
  the transitive path that pulls it in, why it is not exploitable *here*
  ("trusted-input XML only"), and the condition for removal ("remove these when
  upstream catches up"). An ignore is a documented, expiring decision.
- **`[licenses].allow`** — an explicit allowlist; new licenses require a commit.
  `[[licenses.clarify]]` entries handle upstream crates that omit a license
  field, again with the reason and removal condition.
- **`[bans]`** — `multiple-versions = "warn"`.

`renovate.json` automates updates with policy:
- `automerge: true` for minor/patch; **`automerge: false` for major**.
- `helpers:pinGitHubActionDigests` keeps action SHAs pinned automatically.
- Per-package pins with reasons: `evalexpr` capped below v13 because "v13
  relicensed from MIT to AGPL-3.0"; `@tiptap/*` capped because 3.23.x "breaks
  editor lifecycle under real relay latency"; `redis` and `deadpool-redis`
  grouped because "deadpool-redis re-exports redis types".

Every constraint carries the sentence that would otherwise be lost.

## 7. Testing

Four tiers, named and separated in `TESTING.md`:

1. **Unit** — no infrastructure, `just test-unit`.
2. **Integration** — Postgres + Redis, started automatically, `just test`.
3. **E2E** — `#[ignore]`d by default, requires a live relay, run explicitly.
4. **Manual runbooks** — `TESTING.md` is largely a *reproducible manual
   procedure*: build the binaries, start the relay, generate a keypair, create a
   channel, post a message, read it back — with a troubleshooting table mapping
   symptom → cause → fix for ten known failures, including port collisions with
   a developer's own installed app.

Two testing practices stand out:

- **Property tests + adversarial fixtures.** `crates/buzz-conformance/tests/`
  holds `proptest_checker.rs` and committed JSONL fixtures whose names *are* the
  assertion: `good.jsonl`, `bad_host_channel_mismatch.jsonl`,
  `bad_coverage_breach.jsonl`, `bad_foreign_row_leak.jsonl`.
- **Committed golden fixtures with a byte-for-byte contract.** The test
  reconstructs each fixture from typed Rust, asserts the committed file matches
  byte-for-byte "so a schema-change PR must update the fixtures", then replays
  it. Refresh is explicit and opt-in: `BUZZ_CONFORMANCE_UPDATE=1`.

## 8. Formal methods and runtime conformance

The most sophisticated practice in the repository, and the one specd is
structurally best positioned to copy.

`docs/spec/` holds a TLA+ model of the multi-tenant relay
(`MultiTenantRelay.tla` + `.cfg`), a TLA+ model of git-on-object-storage, and a
Tamarin protocol model (`MultiTenantAuth.spthy`). `docs/formal/` holds Python
model programs with their own mutation tests.

`crates/buzz-conformance` then closes the loop between model and running code.
Its doc comment states the north star:

> Don't ask "did the model pass"; ask "did the running code emit a trace the
> model accepts."

The mechanism:

- The relay emits one `TraceStep` per decision at the ingest/auth/read
  accept-reject seam — a **projection** of state, deliberately carrying
  `resolved_community`, `bound_host`, and a 16-hex actor prefix, and deliberately
  *not* carrying client-claimed tags, payloads, keys, or signatures.
- An **independent** checker re-implements the spec's `Next` relation in Rust
  and replays the trace. It calls no production reducer, and it deliberately
  does not reuse production types: sharing normalization helpers "would let a bug
  in the helpers hide itself from both."
- Three failure modes: **illegal transition**, **state mismatch** (the
  non-interference invariant), and **coverage breach**.
- **Coverage breach is load-bearing.** An `EmitGuard` armed at seam entry
  records `ImplBug` on drop if no trace step was emitted. Without it, as the
  crate says, "trace conformance is decorative logging."
- The gate is **observation only** — it never feeds back into the decision, so
  turning it off (`NoopTracer` in production) loses observability and nothing
  else.

`LIMITS.md` then documents what a green run does *not* establish: it is not a
proof; coverage is exactly the executions that ran; it is blind to endpoints
that do not arm a guard; it does not catch DB-layer leaks the projection does
not read, cross-pod leaks, timing properties, pub/sub fan-out, compiler-enforced
type fences, or spec bugs. It states the exact test command and the exact test
count (9 + 5 + 2 = 16) and names the next ratchet.

Two disciplines inside that are worth extracting on their own:

- **Mutation-bite:** "every `TraceAction` variant has a passing case and at
  least one mutation-class bite case." A gate that no mutation can fail is not a
  gate.
- **Named next ratchet:** the doc says which half of the gate is not yet armed
  and what will arm it, rather than implying full coverage.

## 9. Release engineering

Three independent lanes, versioned independently (`RELEASING.md`):

| Lane | Trigger | Artifact |
| --- | --- | --- |
| Desktop | `just release-desktop` → `version-bump/<v>` PR | signed macOS/Linux app |
| Relay | `just release-relay` → `relay-release/<v>` PR | `ghcr.io/block/buzz` image |
| Mobile | `scripts/mobile-release.sh candidate X.Y.Z` | immutable `mobile-vX.Y.Z-rc.N` tag |

Mechanisms:

- **Release-by-PR.** A recipe opens a version-bump PR that bumps manifests,
  regenerates lockfiles, and updates the changelog. Merging it is the human gate.
- **Auto-tag on merge.** `auto-tag-on-release-pr-merge.yml` maps a branch prefix
  to a tag prefix and pushes the tag, which triggers the publishing workflow. The
  tag is created by a dedicated GitHub App token; the default `GITHUB_TOKEN`
  stays read-only "and is never used to create a tag."
- **Immutable candidates.** Mobile RC tags are annotated, protected by a
  ruleset with creation/update/deletion/non-fast-forward protection and one
  always-bypass actor. The troubleshooting section repeatedly says: do not move
  or delete a tag; publish a new one.
- **Manual dispatch is retry-only.** The Release workflow's manual entry "is
  only a retry mechanism for an existing immutable `v<version>` tag. It cannot
  build from `main`."
- **Canary workflow with no release permissions.** `signed-macos-canary` builds
  a signed artifact from `main` for testing, derives a `-test.<run-number>`
  version, and explicitly "has no release permissions, does not create or move
  tags." The doc even notes the artifact is *unpublished, not private*.
- **Honest trade-off statement.** The mobile simplification section names what
  was given up: "trades away a separate stabilization line… Add a dedicated
  hotfix flow later if a release actually needs isolation from `main`."

## 10. Contributor process (`CONTRIBUTING.md`)

- **Table of contents**, twelve numbered sections.
- **"Before You Open a PR"** — search for duplicates and link the closest one
  "or say 'none found'"; open an issue first for anything beyond a small fix.
- **AI-assisted PRs are welcome**, with the contract stated: "No need to
  disclose the tools you used, but you own and must have reviewed the final code.
  Submissions that are clearly unreviewed may be closed."
- **Conventional Commits required** — the PR title becomes the squash-merge
  subject.
- **DCO sign-off required**, enforced by a required check, with the repair
  procedure for a branch that already has unsigned commits.
- **"What a good PR looks like"** — six numbered properties, including "Shows
  the UI" with the reason: "We can't run every branch locally — screenshots let
  us review UI changes same-day."
- **"PRs We're Unlikely to Merge"** — four categories, with the reason: "not
  because they're bad ideas, but because we can't safely review them without
  prior discussion." Followed by "That saves your time as much as ours."
- **Cookbook sections** — "How to Add a New Event Kind" is nine numbered steps,
  each naming the exact file and the exact function to edit. Same for MCP tools
  and HTTP endpoints.

## 11. Operational documentation

- `.env.example` is 11.8KB — a config template that doubles as the config
  reference.
- `TESTING.md` and `RELEASING.md` both end in **symptom → cause → fix**
  troubleshooting tables.
- `preview-features.json` is a machine-readable registry of experimental
  features (`id`, `name`, `description`, `platforms`), so "experimental" is data
  the app reads rather than prose someone remembers.
- `docs/nips/` holds fourteen protocol extension specs, each a first-class
  document for a wire-format decision.
- `perf/RELAY_BUS_SCALING.md` ships with `relay_bus_scaling.py` **and**
  `test_relay_bus_scaling.py` — the capacity model is executable and tested.

## 12. What buzz does that specd should *not* copy

Named here so an agent does not import them by momentum:

- **Scale-driven complexity.** 26 crates, five repos, three client platforms,
  Docker/K8s/Helm. specd is one static binary; none of this applies.
- **A 41KB task runner with ~90 recipes.** The pattern to take is "one command
  reproduces CI", not the recipe count.
- **`just` and `lefthook` as dependencies.** specd is standard-library-only and
  ships one binary; adding two third-party tools to run three commands is the
  wrong trade. Use `make` and `core.hooksPath` (see adoption 01 and 02).
- **Hermit.** Go's toolchain directive in `go.mod` plus `go-version` in CI
  already pins what Hermit pins, for one language.
- **Seven `VISION*.md` files.** specd's scope is one loop; vision documents at
  that volume would be dead vocabulary.
- **TLA+ and Tamarin toolchains.** The *idea* — an independently implemented
  model that judges runtime traces — transfers. The Java-based model checker does
  not fit a standard-library-only Go project (see adoption 09).
- **Screenshot infrastructure.** specd has no UI.
