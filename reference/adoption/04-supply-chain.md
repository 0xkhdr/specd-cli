# 04 — Supply chain and ownership

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P12](../patterns.md#p12--every-rule-names-its-enforcement-and-forbids-the-escape-hatch) | 1 | small | low | not applied |

## Why

buzz runs `cargo-deny check` in a dedicated `security` CI job, backed by
`deny.toml` — an advisory policy, a license allowlist, and a ban list, where
**every exception carries a written reason, the path that pulls it in, why it is
not exploitable in this codebase, and the condition under which it is removed.**

specd's position is different and mostly stronger: zero runtime dependencies,
enforced. But "zero dependencies" is not "zero vulnerabilities." specd compiles
against the Go standard library and ships binaries built by a specific toolchain,
and both receive advisories — `crypto/*`, `net/http`, `archive/*`, `os/exec`,
and the compiler itself have all had them. `govulncheck` is the tool that reads
those advisories against **the code paths specd actually calls**, and it is
distributed by the Go team, run via `go run`, and adds nothing to `go.mod`.

The second gap is ownership: buzz has `.github/CODEOWNERS`; specd has none, so
no review is required on any path, including `release/` and `.github/`.

## Change set

### 4.1 `govulncheck` in CI

Add a job to `.github/workflows/ci.yml`:

```yaml
  # Zero runtime dependencies is not zero advisories: specd links the Go
  # standard library and is built by a pinned toolchain, and both receive
  # them. govulncheck reports only advisories reachable from code specd
  # actually calls, so a finding here is a real call path, not a manifest
  # match. It runs via `go run` and adds nothing to go.mod, which the
  # empty-require-set gate in the repo job continues to enforce.
  vulnerabilities:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.26"
          check-latest: true
      - run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Add `make vuln` to the `Makefile` and include it in `make check`.

**On `@latest`:** it is correct here and nowhere else in this repository. A
vulnerability scanner pinned to a SHA reports a frozen view of the advisory
database, which is the one thing it must never do. Say so in the comment so a
future reader does not "fix" it to match the pinning rule that governs actions.

### 4.2 Handling a finding

Write the policy into `SECURITY.md` before the first finding arrives, modelled
on `deny.toml`'s discipline. A `govulncheck` finding has exactly three legal
resolutions:

1. **Bump the toolchain.** Most standard-library advisories are fixed by a Go
   patch release. Update `go-version` in both workflows and the `go` directive
   in `go.mod` together.
2. **Remove the call path.** If specd's use of the affected symbol is
   incidental, delete it. `AGENTS.md` already prefers deletion.
3. **Record an accepted risk** — only if neither of the above is possible.
   It goes in `release/release-decision.md` as a red gate, in the shape
   `deny.toml` uses: the advisory ID, the exact call path, why it is not
   exploitable in specd's threat model, and the condition that removes it.

There is no fourth option, and specifically no suppression file. buzz's ignores
live in a policy file *with reasons*; a bare suppression with no reason and no
expiry is how a gate stops meaning anything.

### 4.3 `CODEOWNERS`

Create `.github/CODEOWNERS`:

```
# Review routing. Ownership here is not the same as the surface ownership in
# release/surface-inventory.md: that file says which journey or invariant a
# symbol exists for; this file says who must look at a change to it.
*                       @0xkhdr

# Paths where a mistake is not caught by the suite, because these files are
# what the suite trusts.
/.github/               @0xkhdr
/release/               @0xkhdr
/AGENTS.md              @0xkhdr
/SECURITY.md            @0xkhdr
```

The comment distinguishing it from `surface-inventory.md` is load-bearing:
specd already has one file called an ownership mapping, and two files claiming
that word without a distinction is exactly the dead-vocabulary problem
`AGENTS.md` guards against.

### 4.4 Verification instructions already exist — keep them together

`README.md` and `SECURITY.md` both document `sha256sum -c` and
`gh attestation verify`. That is already the level buzz operates at. When
`govulncheck` lands, add one line to `SECURITY.md` §Verifying a release stating
that release builds are also scanned for advisories reachable from called code,
with the same honesty qualifier the rest of the file uses.

## Acceptance

```bash
make vuln     # green, and completes offline-free (it needs network by design)
```

Confirm CI shows `vulnerabilities` as a separate job, so a finding is
distinguishable from a test failure at a glance — the reason buzz gives its
dependency policy its own job.

## Do not

- **Do not add a license policy file.** With an empty require set there is no
  third-party license to allow or deny. If a dependency is ever proposed, the
  gate that fails first is the empty-require-set gate, which is the correct
  place to stop.
- **Do not add Renovate.** `.github/dependabot.yml` already covers GitHub
  Actions and states in a comment why the `gomod` ecosystem is deliberately
  absent. That reasoning is correct and should not be re-litigated.
- **Do not vendor `govulncheck`** or add `golang.org/x/vuln` to `go.mod`. `go
  run` with a version suffix builds it in a scratch module and leaves the
  require set empty.
- **Do not let a `govulncheck` failure be bypassed** by an allowlist flag. If it
  fires, take one of the three resolutions above.

## Deferred

SLSA provenance beyond the existing GitHub attestation, and a signed SBOM. The
current attestation ties a binary to a workflow, repository, and commit, which is
the claim specd actually makes. An SBOM for a zero-dependency static binary would
list the standard library and nothing else.
