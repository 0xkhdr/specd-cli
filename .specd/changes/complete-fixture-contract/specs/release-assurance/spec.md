## ADDED Requirements

### Requirement: Adversarial fixture coverage
Release qualification MUST retain one consumed adversarial fixture for every
protected foundation invariant, and each fixture MUST name the exact refusal
code and single legal next action expected from the exercised route.

#### Scenario: Foundation fixture is missing
- **WHEN** a protected invariant has no committed adversarial fixture
- **THEN** release qualification fails and names the uncovered invariant

#### Scenario: Fixture is not exercised
- **WHEN** a committed release fixture is not consumed by the fixture contract test
- **THEN** release qualification fails instead of accepting dead test data

#### Scenario: Refusal contract drifts
- **WHEN** a fixture-driven route returns a different refusal code or does not carry exactly one legal next action
- **THEN** the fixture contract test fails with the mismatched case
