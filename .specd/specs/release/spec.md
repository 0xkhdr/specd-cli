# Release

## Purpose

State what a pushed tag must already satisfy before the release workflow spends
a gate run, a build, or an attestation on it.

### Requirement: Tag contract precedes release work
The system MUST refuse a pushed tag that does not satisfy the release contract
before the gates, the build, or the attestation run.

#### Scenario: Lightweight tag is refused
- **WHEN** a pushed tag is not an annotated tag
- **THEN** the release workflow fails before the gates job starts, naming the
  tag as lightweight

#### Scenario: Missing changelog section is refused early
- **WHEN** a pushed tag names a version with no section in CHANGELOG.md
- **THEN** the release workflow fails before the gates job starts, naming the
  version whose section is missing

#### Scenario: A tag meeting the contract proceeds
- **WHEN** a pushed annotated tag names a version with a CHANGELOG.md section
- **THEN** the gates job runs and the release proceeds unchanged
