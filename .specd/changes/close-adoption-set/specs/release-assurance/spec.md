## ADDED Requirements

### Requirement: Observed platform claims
The typed maturity registry MUST agree with the release decision on which
platforms are supported, and every platform claim MUST carry the date of the
raced-suite observation that record states.

#### Scenario: Unsupported platform claims proven
- **WHEN** a platform row is proven and the release decision does not list it as supported
- **THEN** release qualification fails and names the platform and the record that disagrees

#### Scenario: Supported platform claims less
- **WHEN** the release decision supports a platform and its registry row is not proven
- **THEN** release qualification fails instead of publishing a level the record contradicts

#### Scenario: Platform claim is dated by hand
- **WHEN** a platform row carries a date other than the recorded raced-suite observation
- **THEN** release qualification fails and names both dates

### Requirement: Actionable state refusal
Every operation that loads a change's canonical state MUST refuse a missing or
unreadable state file with one stable refusal code and exactly one legal next
action, and that action MUST NOT be the operation that produced the refusal.

#### Scenario: Change directory has no state
- **WHEN** any state-loading operation runs against a change directory with no state file
- **THEN** it refuses with the stable state refusal code and one legal next action, changing no bytes

#### Scenario: Recovery does not name the failing operation
- **WHEN** a state-loading operation refuses an unreadable state file
- **THEN** the next action names a different, legal step rather than rerunning the same command
