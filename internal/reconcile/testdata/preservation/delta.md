## MODIFIED Requirements

### Requirement: Replace me

The system MUST be replaced by this complete block.

#### Scenario: New
- **WHEN** the new behavior runs
- **THEN** the new result appears

## REMOVED Requirements

### Requirement: Drop me

**Reason**: the behavior is obsolete
**Migration**: no caller depends on it

## RENAMED Requirements

- FROM: `### Requirement: Old name`
- TO: `### Requirement: New name`

## ADDED Requirements

### Requirement: Fresh behavior

The system SHALL append new behavior last.

#### Scenario: Fresh
- **WHEN** reconciliation inserts it
- **THEN** it appears after existing requirements
