# gap analysis — specd against buzz

specd is not a small version of buzz. It is a smaller, stricter project that
already beats buzz on several axes and is behind it on others. This document
separates the two so no adoption item "fixes" something that is already better.

## specd's measured baseline

- 173 Go files, 35,980 lines, 83 of those files are tests.
- Zero runtime dependencies; `go.mod` has an empty require set, gated in CI.
- CI matrix: `ubuntu-latest`, `ubuntu-24.04-arm`, `macos-latest`,
  `windows-latest`. All actions pinned to commit SHAs.
- Release workflow: tag contract self-check → four-platform gate re-run →
  `-trimpath` reproducible build → SHA256SUMS → build provenance attestation.
- Fourteen release journeys replayed over committed fixtures.
- A mechanical release-qualification test parsing repository facts.
- A subtraction test proving every exported symbol has a named owner.

## Where specd already exceeds buzz

Do not regress these while adopting anything below.

| Axis | specd | buzz |
| --- | --- | --- |
| Dependency surface | zero runtime deps, gated | 255KB `Cargo.lock`, 260KB `pnpm-lock.yaml`, four ignored advisories |
| Surface ownership | every exported symbol maps to a journey, invariant, or contract; unowned surface fails the build | no equivalent; surface grows freely |
| Release claim discipline | a platform claim is earned by a green run and recorded with its date; `v0.1.0` was **retracted in `go.mod`** because its record overstated two platforms | claims are prose in `RELEASING.md` |
| Deletion enforcement | `subtraction_test.go` fails when a row describes surface that no longer exists — bidirectional | `dead-token-guard` is one grep for one past deletion |
| Documentation parity | `docs/operations.md` is byte-compared against the registry projection; broken relative links fail the build | docs drift is caught by review |
| Determinism | no network or LLM import permitted in the deterministic core, enforced by parsing imports | not applicable |
| Reproducibility | `-trimpath`, pinned toolchain, tag as the only version input | signed but not reproducible |

specd's `release/release-decision.md` is a stronger artifact than anything in
buzz: it names which gates stopped running and why, states that a claim is
earned by a run rather than a runner, and records that a traversal "is not
verifiable by anyone but its operator." That is the standard the rest of this
work should be held to.

## Where specd is behind — ranked

### Tier 1 — closes a gap specd's own documents admit

| Gap | Evidence in specd | buzz reference | Adoption |
| --- | --- | --- | --- |
| No proof the gates bite | 83 test files, no mutation discipline | `LIMITS.md`: "every variant has a passing case and at least one mutation-class bite case" | [05](adoption/05-mutation-bite-tests.md) |
| No fuzzing of parsers | `internal/plan/parse.go`, `tasks.go`, `internal/core/record`, `state` all parse untrusted-shaped input; zero `func Fuzz` in the repo | `proptest_checker.rs` | [06](adoption/06-fuzz-and-property-tests.md) |
| No model-level conformance | the lifecycle is a state machine; correctness is asserted by example journeys only | `buzz-conformance` — independent checker, coverage breach, `LIMITS.md` | [09](adoption/09-model-conformance.md) |
| Scale explicitly unproven | README: "scale and long-lived changes are not yet proven"; zero `func Benchmark` | `perf/RELAY_BUS_SCALING.md` + executable, tested capacity model | [10](adoption/10-benchmarks-and-scale.md) |
| Per-gate limits undocumented | `release-decision.md` records gate status, not gate blind spots | `crates/buzz-conformance/LIMITS.md` | [08](adoption/08-limits-docs.md) |

### Tier 2 — process and reproducibility

| Gap | specd today | buzz reference | Adoption |
| --- | --- | --- | --- |
| No single local gate command | contributors memorize two commands plus a third for regeneration | `just ci` | [01](adoption/01-local-gate-runner.md) |
| No git hooks | nothing prevents pushing unformatted code | `lefthook.yml` — auto-fix at commit, verify at push | [02](adoption/02-git-hooks.md) |
| CI has no per-job timeout | a hung job burns the full runner budget | `timeout-minutes` on all fourteen jobs | [03](adoption/03-ci-hardening.md) |
| No vulnerability scan | zero deps ≠ zero CVEs; the Go standard library and toolchain get advisories | `cargo-deny check` in a dedicated `security` job | [04](adoption/04-supply-chain.md) |
| No `CODEOWNERS` | review routing is implicit | `.github/CODEOWNERS` | [04](adoption/04-supply-chain.md) |
| Golden fixtures have no byte contract | `SPECD_WRITE_*` exists for generated docs and agent JSON, not for journey fixtures | fixtures asserted byte-for-byte; `BUZZ_CONFORMANCE_UPDATE=1` to refresh | [07](adoption/07-golden-fixture-contract.md) |
| Release tagging is manual | a human pushes the tag | `auto-tag-on-release-pr-merge.yml`, App-token tagging, retry-only dispatch | [11](adoption/11-release-automation.md) |

### Tier 3 — documentation completeness

| Gap | specd today | buzz reference | Adoption |
| --- | --- | --- | --- |
| No architecture document | `docs/concepts.md` explains the model; nothing explains the code's shape | `ARCHITECTURE.md`, 827 lines, nine sections | [12](adoption/12-architecture-doc.md) |
| "Experimental" is prose | README says the production profile is experimental; nothing machine-checks it | `preview-features.json` | [13](adoption/13-experimental-registry.md) |
| No commit or PR contract | templates exist; no stated commit convention, duplicate-search rule, or unlikely-to-merge list | `CONTRIBUTING.md` §Before You Open a PR | [14](adoption/14-contributor-contract.md) |
| No cookbook for extension | `docs/contributing.md` describes the build; nothing gives numbered steps for "add an operation" | "How to Add a New Event Kind", nine numbered steps | [14](adoption/14-contributor-contract.md) |

## Deliberate non-gaps

Not missing. Do not open work for them.

- **No `dead-token-guard` equivalent.** `subtraction_test.go` already fails when
  a row describes surface that no longer exists, which is the general form of
  what buzz's grep does for one case.
- **No Renovate.** Dependabot is configured for GitHub Actions, and the `gomod`
  ecosystem is deliberately absent because the empty require set is a gate. That
  reasoning is already written into `.github/dependabot.yml`.
- **No path-filtered CI.** buzz filters because it has five independent
  surfaces and 45-minute jobs. specd has one module and a suite that runs in
  minutes. Filtering would add a way for a job to be skipped when it should not
  be, in exchange for nothing.
- **No license policy file.** With an empty require set there is no third-party
  license to allow or deny. If a dependency is ever added, the gate that fails
  first is the empty-require-set gate, which is the correct place to stop.
- **No Hermit.** `go.mod`'s `go 1.26` directive plus `go-version: "1.26"` in
  both workflows already pins the one toolchain that exists.
- **No `.env.example`.** specd reads five environment variables, all documented
  in generated operations output and `docs/`.
