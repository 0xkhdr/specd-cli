## MODIFIED Requirements

### Requirement: Stable locking
The account store MUST serialize updates.

#### Scenario: Concurrent updates
- **WHEN** two account updates begin together
- **THEN** both updates commit without lost bytes
