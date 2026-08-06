# Release journey fixtures

Each directory is committed input driven through a release journey or the
fixture contract's real CLI/core route. Happy paths use `good_<case>`;
adversarial inputs use `bad_<invariant>_<violation>`. Each named case carries a
schema, scenario, protected invariant, and, for refusals, the exact code and
legal recovery instruction.

These inputs are authored, not generated, so they have no refresh switch. A
fixture change is reviewed with its route. `TestReleaseFixtureContract`
discovers every `good_*` and `bad_*` directory, rejects unreadable or malformed
cases, and fails when any of the eight protected invariants has no `bad_*`
case. Every new invariant therefore needs a consumed adversarial fixture in the
same commit.
