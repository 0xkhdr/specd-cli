# Documentation

## Purpose

Help developers evaluate, adopt, operate, integrate, and contribute to specd
using documentation that is readable, current, and honest about its guarantees.

### Requirement: Audience-oriented navigation
The documentation MUST provide a concise root README and one documentation hub
that route evaluators, users, agent integrators, and contributors to the right
page without duplicating full reference material.

#### Scenario: Reader finds the right starting point
- **WHEN** a reader opens the root README or `docs/README.md`
- **THEN** the page identifies the relevant audience path, expected outcome,
  and next document to read

### Requirement: Consistent developer-friendly voice
Every hand-written documentation page MUST use the project vocabulary, direct
plain language, scannable sections, and enough context for its intended reader
without assuming knowledge supplied only by another duplicate introduction.

#### Scenario: Developer moves between pages
- **WHEN** a developer follows links across the documentation set
- **THEN** terminology, command naming, tone, and explanations remain
  consistent while each fact has one clear owner

### Requirement: Current operational guidance
Commands, examples, file layouts, lifecycle descriptions, refusal recovery,
agent integration guidance, and contributor instructions MUST agree with the
current registry, source, tests, and generated operation reference.

#### Scenario: Documentation is checked against the repository
- **WHEN** the documentation review and release qualification checks run
- **THEN** relative links resolve, generated operation documentation has byte
  parity, examples use current syntax, and behavioral claims have a current
  implementation or test owner

### Requirement: Honest production boundary
The documentation MUST help a developer assess production use by separating
implemented guarantees from experimental profiles, supported platforms,
advisory host checks, and unproven scale or concurrency claims.

#### Scenario: Evaluator assesses production use
- **WHEN** a reader looks for deployment or production guidance
- **THEN** the docs link to the security policy and release decision and do not
  imply assurance beyond the evidence recorded there
