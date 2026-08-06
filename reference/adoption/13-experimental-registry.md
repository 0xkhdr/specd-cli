# 13 — Make "experimental" data instead of prose

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P13](../patterns.md#p13--machine-readable-registry-for-anything-currently-claimed-in-prose) | 4 | small | low | not applied |

## Why

buzz keeps `preview-features.json` — a machine-readable registry of experimental
features, each with `id`, `name`, `description`, and `platforms`. "Experimental"
is data the app reads, not a sentence someone remembers to update.

specd's maturity claims are currently prose, repeated in four places:

| Claim | Stated in |
| --- | --- |
| "The production profile is experimental" | `README.md`, `docs/README.md` |
| "the base loop is proven end to end on linux/amd64" | `README.md`, `SECURITY.md`, `release/release-decision.md` |
| "Host assurance is `advisory` unless the host provides containment" | `README.md`, `SECURITY.md`, `docs/README.md`, the generated `AGENTS.md` block |
| "driving the loop end to end from two callers at once is still not claimed" | `README.md`, `release/release-decision.md` |

Four documents, one truth, four chances to drift. This is exactly the problem
specd already solved for operations: one registry projects `--help`,
`docs/operations.md`, and the managed `AGENTS.md` block. Maturity is the
remaining hand-maintained claim, and the mechanism to fix it already exists in
`internal/generate`.

There is a second, sharper reason. The generated `AGENTS.md` block tells agents:
"Unless your host proves otherwise, treat every such guarantee as `advisory`…
Do not report an advisory guarantee as an enforced one." An agent is being asked
to distinguish advisory from enforced guarantees using prose. Making the
assurance level structured data means the agent reads a value rather than
interpreting a sentence.

## Change set

### 13.1 The registry

A Go declaration in the deterministic core, not a JSON file — specd's registries
are already Go, `internal/generate` already projects them, and a JSON file would
need a parser, a schema, and an embed for no gain:

```go
// internal/core/maturity.go
//
// The one source for every maturity and assurance claim specd publishes.
// README.md, SECURITY.md, docs/, and the generated agent guidance all project
// this; none of them states a maturity fact of its own. A claim that is not
// here is a claim specd does not make.

type Assurance string

const (
	AssuranceAdvisory Assurance = "advisory" // checked by the harness, not enforced by it
	AssuranceEnforced Assurance = "enforced" // the host proves containment
)

type Maturity string

const (
	MaturityProven       Maturity = "proven"       // driven end to end by hand, dated
	MaturityGated        Maturity = "gated"        // suite and journeys green in CI, dated
	MaturityExperimental Maturity = "experimental" // shipped, not qualified
	MaturityUnclaimed    Maturity = "unclaimed"    // deliberately not claimed
)
```

Then the entries: one per platform, one per profile (default / production), one
per guarantee class (approval, evidence, scope, atomicity, fail-closed, path
containment, host assurance). Each carries its level, the date it was observed,
and the evidence reference — the same three fields `release/release-decision.md`
already uses in prose, promoted to fields.

### 13.2 Project it

- **Generated guidance.** `internal/generate` renders the assurance section of
  the managed `AGENTS.md` block from the registry instead of from a fixed
  template string. The parity gate then covers maturity claims automatically —
  which is the whole point.
- **`report --kind status`.** Already projects deferred-domain friction
  eligibility as a fact for a root owner to weigh; the assurance level is the
  same class of fact and belongs in the same projection. Route it through
  `cmd.RenderJSON`/`cmd.RenderText`, not a new helper.
- **`--version`.** Consider adding the default-profile assurance level to
  verbose version output. Consider, not necessarily do — it is user-visible
  surface and needs an owner row.
- **Documentation.** `README.md`, `SECURITY.md`, and `docs/README.md` link to
  the projected claim rather than restating it. Where a prose sentence must
  remain for readability, add a release-gate check that the stated level matches
  the registry — the same technique `release_test.go` already uses to parse
  `release-decision.md`.

### 13.3 Gate it

Extend `internal/integration/release_test.go`:

- every guarantee named in `SECURITY.md` §What specd defends has a registry
  entry;
- every registry entry at `proven` carries a date and an evidence reference;
- no document states a maturity level that contradicts the registry.

### 13.4 Ownership

New exported surface — `Assurance`, `Maturity`, the entry type, and the registry
accessor — needs rows in `release/surface-inventory.md`. The natural owner is
`invariant:validation` for the registry itself and `journey:12` for the
assurance projection, since journey 12 already owns "the human gate and honest
host assurance at handoff." Verify against the real table rather than trusting
this suggestion.

## Acceptance

```bash
go test ./... -race -count=1
SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity
git diff --quiet   # regeneration on a green tree produces no diff
```

Then the real test: change one registry entry from `gated` to `proven` without
adding a date, and confirm the release gate fails. Change a maturity sentence in
`README.md` to contradict the registry, and confirm the gate fails. If neither
fails, the registry is decoration.

## Do not

- **Do not add a JSON file.** buzz uses JSON because a TypeScript desktop app
  and a Flutter app both read it. specd has one consumer, written in Go.
- **Do not introduce new vocabulary.** `assurance`, `advisory`, and `enforced`
  are already in the generated guidance; `maturity`, `proven`, `gated`, and
  `experimental` are already in `README.md` and `release-decision.md`. Nothing
  new appears on user- or agent-visible surface, so the dead-vocabulary gate
  stays green — confirm that rather than assuming it.
- **Do not let the registry become a feature-flag system.** It records claims,
  not behavior. Nothing branches on it. A flag that changes what specd *does* is
  a bypass by another name, and `AGENTS.md` forbids those.
- **Do not upgrade a level without the observation.** `proven` requires a hand
  traversal on that platform; `gated` requires a green run. That distinction is
  the reason `v0.1.0` was retracted, and encoding it in a type makes it harder
  to blur, not easier.

## Deferred

Per-operation maturity (marking individual operations experimental). The
production profile is already the unit of experimentation, and per-operation
levels would be finer than any claim specd currently makes.
