# Release journey fixtures

Each directory is committed input driven through a release journey. New happy
paths use `good_<case>`; adversarial inputs use
`bad_<invariant>_<violation>`. Tests assert the exact refusal code and its one
legal next action.

These inputs are authored, not generated, so they have no refresh switch. A
fixture change is reviewed with its journey. Every new invariant needs a
`bad_*` journey fixture in the same commit, and every fixture must be read by a
test.
