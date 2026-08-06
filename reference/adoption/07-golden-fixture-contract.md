# 07 — Golden fixtures as a byte contract

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P6](../patterns.md#p6--golden-fixtures-are-a-byte-contract-with-an-explicit-refresh-switch), [P7](../patterns.md#p7--adversarial-fixtures-named-for-what-they-violate) | 2 | small | low | applied 2026-08-06 |

## Why

buzz commits its conformance fixtures and asserts them byte-for-byte, with a
stated reason — "so a schema-change PR must update the fixtures" — and refreshes
them only under `BUZZ_CONFORMANCE_UPDATE=1`. Its fixture names *are* the
assertions: `good.jsonl`, `bad_host_channel_mismatch.jsonl`,
`bad_coverage_breach.jsonl`, `bad_foreign_row_leak.jsonl`.

specd already has both halves in the repository but has not joined them:

- The **refresh switch** exists — `SPECD_WRITE_OPERATION_DOCS`,
  `SPECD_WRITE_AGENT_JSON` — and the operations document is already byte-compared
  against its projection as a release gate.
- The **fixtures** exist — `internal/integration/testdata/release/`,
  `internal/plan/testdata/`, `internal/cmd/testdata/`,
  `internal/reconcile/testdata/`.

What is missing is the same byte contract over the journey and plan fixtures,
and a fixture naming convention that lets a reader enumerate the covered
violations from the tree alone.

## Change set

### 7.1 Extend the refresh convention

Adopt one variable per fixture family, matching the existing `SPECD_WRITE_*`
naming:

| Variable | Refreshes |
| --- | --- |
| `SPECD_WRITE_OPERATION_DOCS` | `docs/operations.md` (exists) |
| `SPECD_WRITE_AGENT_JSON` | the agent JSON golden (exists) |
| `SPECD_WRITE_JOURNEY_FIXTURES` | `internal/integration/testdata/release/` |
| `SPECD_WRITE_PLAN_FIXTURES` | `internal/plan/testdata/` expected outputs |

Every fixture assertion follows one shape:

```go
// Byte-compared, not semantically compared: a schema or rendering change must
// update the fixture in the same commit, which is what makes the fixture a
// review artifact rather than a cache. Refresh is explicit and opt-in.
func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("SPECD_WRITE_PLAN_FIXTURES") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing fixture %s (refresh with SPECD_WRITE_PLAN_FIXTURES=1): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture %s is stale; refresh with SPECD_WRITE_PLAN_FIXTURES=1", path)
	}
}
```

Two details that carry the whole value:

- The **failure message names the refresh command.** A contributor who cannot
  find it will hand-edit the fixture until the test passes, which defeats it.
- The refresh path **returns without asserting**. A run that writes must not
  also report success on a comparison it did not perform.

Note the deliberate limit, and write it into the helper's comment: because
refresh is a switch, a contributor *can* refresh a fixture that changed for a
bad reason. The fixture makes the change visible in review; it does not make it
correct. That is the same limit buzz accepts.

### 7.2 Name adversarial fixtures for what they violate

Rename or add fixtures under `internal/integration/testdata/release/` so the
tree enumerates the invariants. The eight foundation invariants from
`release/surface-inventory.md` are the axis:

```
testdata/release/
  good/…
  bad_approval_self_granted/…
  bad_authority_forged_actor/…
  bad_scope_undeclared_write/…
  bad_evidence_stale_head/…
  bad_evidence_not_applicable/…
  bad_staleness_revision_moved/…
  bad_atomicity_torn_write/…
  bad_validation_malformed_tasks/…
  bad_failclosed_future_schema/…
```

Each `bad_*` fixture's test asserts the specific refusal code and its one legal
next action, joining this item to
[adoption 05](05-mutation-bite-tests.md). Renames must be reflected in
`internal/integration/release_journeys_test.go` and in the `requiredJourneys`
list, and any journey identifier printed in `release/release-decision.md` must
move in the same commit — that document is parsed by `release_test.go`, so a
mismatch fails the build, which is the correct outcome.

### 7.3 Add a fixture README

`internal/integration/testdata/release/README.md`, short:

- what a fixture is (a committed input plus its expected refusal or result);
- the naming convention (`good_*` / `bad_<invariant>_<violation>`);
- the refresh command and the warning that refreshing is not review;
- the rule that a new invariant gets a `bad_*` fixture in the same commit.

## Acceptance

```bash
go test ./... -count=1                                    # green
printf 'x' >> internal/plan/testdata/<some-expected-file> # corrupt a fixture
go test ./internal/plan -count=1                          # fails, naming the refresh command
git checkout internal/plan/testdata
SPECD_WRITE_PLAN_FIXTURES=1 go test ./internal/plan -count=1
git diff --quiet                                          # a green suite leaves no diff
```

The last line is the real assertion: if refreshing a green suite produces a
diff, the generator and the comparison disagree and one of them is wrong.

## Do not

- Do not compare fixtures semantically ("parse both, compare structs"). That
  hides rendering, ordering, and whitespace changes — the changes a golden
  fixture exists to surface.
- Do not refresh a fixture to make a failing test pass without reading the diff.
  The diff *is* the finding.
- Do not add a fixture that no test reads. `subtraction_test.go`'s bidirectional
  rule is the house style: a dead fixture is the same defect as an unowned
  symbol.
- Do not put a real `.specd/` root under `testdata/` unless a test drives it;
  harness-owned state in the tree is what the 2026-08-01 publishing cleanup
  removed on purpose.

## Deferred

Full generated `.specd` root snapshots. Release journey inputs are authored,
and generating them would blur input with expected output. The automatic
fixture-coverage gate is no longer deferred: `TestReleaseFixtureContract`
discovers the tree and compares it with the protected invariant list.

## Acceptance note — 2026-08-06

`TestPlanGoldenByteContract` byte-compares one stable semantic projection and
names `SPECD_WRITE_PLAN_FIXTURES=1` on drift. Fixture READMEs define
`good_<case>` and `bad_<invariant>_<violation>`. Existing journey directories
remain authored inputs: no write switch was added because regenerating authored
Markdown would erase the distinction between input and expected output.

`TestReleaseFixtureContract` now consumes one good case and nine adversarial
cases covering all eight protected invariants. Each adversarial case drives a
real CLI or core refusal route and compares the exact refusal code and recovery
instruction. Directory discovery fails on dead or malformed named fixtures,
and coverage fails when a protected invariant has no case. Verified with:

```bash
go test ./internal/integration -run 'TestReleaseFixtureContract|TestReleaseQualification|TestSurfaceOwnership' -count=1
```
