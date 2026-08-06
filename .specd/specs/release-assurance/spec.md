# Release assurance

## Purpose

Keep code-shape guidance, published maturity claims, and contribution policy
checkable from repository facts.

### Requirement: Architecture documentation
The repository MUST explain the implemented package layers, one-owner rules,
durability and trust boundaries, limitations, and reversal conditions, and MUST
mechanically validate its relative links.

#### Scenario: Architecture link drifts
- **WHEN** a relative link in `ARCHITECTURE.md` does not resolve
- **THEN** release qualification fails with the broken source and target

### Requirement: Canonical maturity claims
The deterministic core MUST own each published platform, profile, and guarantee
claim with a level, observation date, and evidence reference, and existing
guidance and status-report surfaces MUST project those claims.

#### Scenario: Proven claim loses evidence
- **WHEN** a claim is marked proven without a date or evidence reference
- **THEN** release qualification fails and forbids release

#### Scenario: Documentation contradicts a claim
- **WHEN** user or security documentation states a maturity or assurance level
  that disagrees with the registry
- **THEN** release qualification fails and forbids release

### Requirement: Enforced contributor contract
Contributor documentation MUST describe the merge policy, agent-authored change
responsibility, refusal boundaries, and exact operation, gate, and refusal
extension workflows, and MUST name enforcement for every rule.

#### Scenario: Contributor follows an extension cookbook
- **WHEN** a contributor adds an operation, gate, or refusal
- **THEN** one numbered workflow identifies the canonical owner, projection,
  bite test, inventory, regeneration, and verification steps

### Requirement: Runtime conformance coverage
Release qualification MUST judge every executable operation and retained
journey against an independent test-local model and MUST fail closed when an
execution is missing, malformed, illegal, or inconsistent with modeled state.

#### Scenario: Operation produces no valid result envelope
- **WHEN** a release-journey operation reaches the CLI driver without a valid result envelope
- **THEN** conformance fails with a coverage breach instead of accepting an incomplete step

#### Scenario: Operation violates its modeled contract
- **WHEN** an observed or generated operation uses an illegal actor or lifecycle or produces the wrong state transition
- **THEN** conformance fails with an illegal-transition or state-mismatch result

#### Scenario: Required behavior is not exercised
- **WHEN** an executable operation, retained journey, or required refusal class emits no step
- **THEN** conformance fails with a coverage breach naming what was not exercised

#### Scenario: Committed trace fixture drifts
- **WHEN** a typed conformance case differs from its committed JSONL bytes
- **THEN** the test fails and names `SPECD_WRITE_CONFORMANCE_FIXTURES=1` as the only refresh action
