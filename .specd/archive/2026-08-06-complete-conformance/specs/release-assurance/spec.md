## ADDED Requirements

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
