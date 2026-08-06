## ADDED Requirements

### Requirement: Managed-name documentation parity
The project MUST document every category of managed path name that
`path.ValidateSegment` refuses as reserved.

#### Scenario: Windows device name is evaluated on any host
- **WHEN** a reader chooses a managed name using the layout or troubleshooting documentation
- **THEN** the documentation identifies Windows device names as reserved on every platform and directs the reader to choose another name
