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
