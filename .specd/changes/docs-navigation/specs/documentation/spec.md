## Purpose
Help a reader choose the right entry point and understand specd's workflow
without reading duplicate or competing introductions.

## ADDED Requirements

### Requirement: Audience-oriented entry points
The documentation MUST provide a concise root README for product orientation
and installation, plus a documentation index that routes users by goal and
recommended reading order.

#### Scenario: New user finds the first workflow
- **WHEN** a reader opens the repository README to evaluate or install specd
- **THEN** the README explains what specd is, shows the shortest useful
  workflow, and links to the getting-started guide without duplicating the
  full command reference

### Requirement: Single ownership of documentation navigation
The documentation MUST keep navigation and audience routing in `docs/README.md`
while the detailed topic pages remain linked by their subject.

#### Scenario: Reader chooses a path
- **WHEN** a reader opens `docs/README.md`
- **THEN** the page identifies paths for using specd, understanding its model,
  driving it from an agent, troubleshooting it, and contributing to it

### Requirement: Existing claims remain linked and verifiable
The documentation MUST link the generated operation reference, binding agent
guide, release decision, security policy, and changelog from an appropriate
entry point without changing their claims.

#### Scenario: Release documentation checks the new structure
- **WHEN** the repository documentation link and release qualification checks
  run
- **THEN** all relative links resolve and generated operation documentation is
  unchanged
