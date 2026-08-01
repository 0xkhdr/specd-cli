# Security

## Reporting a vulnerability

Report privately through GitHub Security Advisories on this repository
("Security" → "Report a vulnerability"). Please do not open a public issue for
anything you believe is exploitable.

Include what you ran, what you expected the harness to refuse, and what it did
instead. A reproduction against a scratch root is worth more than a description.

This is a young project maintained by one person, and `0.x` is an honest
maturity claim rather than a formality. Expect a first response within a week;
there is no paid support and no guaranteed patch window. Fixes land on the
default branch and in the next tag — there are no backport branches.

## Supported versions

Only the latest tag, and only `linux/amd64`. The suite and the fourteen release
journeys also run on `linux/arm64`, macOS, and Windows, and all four gate every
release, but no change has been driven through the loop by hand on anything but
`linux/amd64`. See
[`release/release-decision.md`](release/release-decision.md) for the limits of
what each tier establishes.

## Verifying a release

Release binaries carry a build provenance attestation tying them to the
workflow, repository, and commit that produced them:

```bash
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify specd_linux_amd64 --repo 0xkhdr/specd-cli
```

Workflow dependencies are pinned to commit SHAs rather than mutable tags, so a
compromised upstream tag cannot silently enter a release build.

## What specd defends

These are the guarantees a bypass in would be a vulnerability:

- **Approval cannot be self-granted.** The human route is derived from a termios
  ioctl on stdin, so only a real controlling terminal derives it and every other
  stdin derives agent. Any path that lets an agent-routed invocation pass
  `approve` or `sync` is a vulnerability.
- **Evidence cannot be forged or reused.** Verification evidence is an
  observation pinned to current HEAD. Any path that completes a task without
  applicable passing evidence, or that makes stale evidence look applicable, is
  a vulnerability.
- **Declared scope is enforced.** A task names the files it may touch, and
  git-ignored files count. Any path that lets a write outside the declared set
  pass `verify` is a vulnerability.
- **Managed state is old-or-new.** `.specd/` state, history, evidence, and task
  markers are written atomically under revision guards. Any interruption that
  leaves a torn, partially applied, or silently rolled-forward document is a
  vulnerability.
- **Refusals fail closed.** Any refusal path that fails open — proceeding on
  ambiguous, corrupt, or future state — is a vulnerability.
- **Path containment.** Any change name, capability name, or root selection that
  escapes the managed root through traversal or a reserved segment is a
  vulnerability.

## What specd does not defend

Not vulnerabilities. They are the stated boundary, and reports about them will
be closed as working-as-designed:

- **`specd verify` runs commands your plan declares, as you, without a shell.**
  That is the feature. A task that declares a hostile command runs a hostile
  command. specd does not sandbox it, does not review it, and never inspects it
  for intent. Review a plan before you approve it — that is what approval is.
- **The harness declares scope; it does not isolate a process.** Only a
  conformant host can contain one. Host assurance is `advisory` unless the host
  proves stronger containment, and specd says so rather than implying more.
- **`SPECD_ROUTE` is a host declaration, not proof.** It is provenance. The
  harness can refuse an agent that declared itself; it can never attest that a
  human is at the keyboard. A host that lies about its route only fools itself,
  and declaring a human terminal raises no assurance label.
- **A local user with write access to `.specd/` can rewrite it.** State is
  harness-owned by convention and by atomic writes, not by filesystem
  permissions or by cryptographic signature. specd defends against interrupted
  and concurrent writes, not against a person with an editor. Its answer to a
  hand-edited root is to fail closed, not to detect tampering.
- **Anything outside the managed root.** specd makes no claim about your Git
  remote, your CI credentials, your agent's network access, or what the agent
  does with the context it was handed.
- **Concurrency at scale.** The root and change locks serialize managed writes,
  but the release boundary does not claim proof under concurrent callers. A
  concurrency defect is a real bug worth reporting — just not a surprise.

## No telemetry, no network

No validation, state, graph, evidence, or report path makes a network call or
invokes an LLM, and a release gate parses the imports of the deterministic core
to keep it that way. specd sends nothing anywhere. The only process it starts is
the verification argv your own task declared.
