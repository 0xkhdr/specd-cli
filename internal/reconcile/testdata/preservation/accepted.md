# Cache policy

## Purpose

Cache behavior of the gateway.

### Requirement: Keep unrelated

The system MUST   keep unrelated bytes exactly as authored.

#### Scenario: Untouched
- **WHEN** reconciliation rebuilds the document
- **THEN** these bytes are unchanged

### Requirement: Replace me

The system MUST be replaced completely.

#### Scenario: Old
- **WHEN** the old behavior runs
- **THEN** the old result appears

### Requirement: Drop me

The system MUST go away.

#### Scenario: Gone
- **WHEN** removed
- **THEN** absent

### Requirement: Old name

The system MUST keep its body across a rename.

#### Scenario: Kept
- **WHEN** renamed
- **THEN** the body is preserved
