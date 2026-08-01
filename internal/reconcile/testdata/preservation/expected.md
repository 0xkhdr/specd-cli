# Cache policy

## Purpose

Cache behavior of the gateway.

### Requirement: Keep unrelated

The system MUST   keep unrelated bytes exactly as authored.

#### Scenario: Untouched
- **WHEN** reconciliation rebuilds the document
- **THEN** these bytes are unchanged

### Requirement: Replace me

The system MUST be replaced by this complete block.

#### Scenario: New
- **WHEN** the new behavior runs
- **THEN** the new result appears

### Requirement: New name

The system MUST keep its body across a rename.

#### Scenario: Kept
- **WHEN** renamed
- **THEN** the body is preserved

### Requirement: Fresh behavior

The system SHALL append new behavior last.

#### Scenario: Fresh
- **WHEN** reconciliation inserts it
- **THEN** it appears after existing requirements
