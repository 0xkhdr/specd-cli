# 06 — Fuzz the parsers, property-test the transitions

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P8](../patterns.md#p8--fuzz-what-parses-property-test-what-transitions) | 2 | medium | low | not applied |

## Why

buzz property-tests its transition checker (`proptest_checker.rs`) and commits
adversarial fixtures whose filenames name the violation.

specd has **zero** `func Fuzz` and zero property tests, while parsing several
classes of externally shaped input:

| Input | Parser | Written by | Trust |
| --- | --- | --- | --- |
| `proposal.md`, `design.md`, `tasks.md` | `internal/plan/parse.go`, `sections.go`, `tasks.go` | an agent | untrusted shape |
| `specs/<capability>/spec.md` deltas | `internal/plan/deltas.go` | an agent | untrusted shape |
| `state.json` | `internal/core/state` | the harness | trusted writer, hostile filesystem |
| `history.jsonl`, `evidence.jsonl` | `internal/core/record` | the harness | append-interruptible |
| change / capability / root names | `internal/core/path` | a caller | untrusted, and a containment boundary |

The two rows that matter most are the last two. `internal/core/record` must
survive a torn append — `journey:04` covers interrupted append and replay by
example, and fuzzing generalizes it. `internal/core/path` is the containment
boundary: `SECURITY.md` states that "any change name, capability name, or root
selection that escapes the managed root through traversal or a reserved segment
is a vulnerability." That is a fuzz target with a security-grade oracle.

Go's native fuzzing (`testing.F`, Go 1.18+) needs no dependency.

## The oracle

The most common fuzzing mistake is asserting "no error." specd's published
contract gives a much stronger oracle. Every target asserts:

1. **No panic.** Free — the fuzzer reports it.
2. **Every refusal carries exactly one legal next action.** This is specd's
   published guarantee, so it is the correct invariant to fuzz against.
3. **Round-trip stability where one exists.** Parse → render → parse produces an
   identical model.
4. **Containment** (path targets only). Every accepted name resolves to a path
   inside the managed root; no accepted input escapes via traversal, absolute
   path, symlink component, reserved segment, or platform-specific device name.

## Change set

### 6.1 Path containment — write this one first

`internal/core/path/path_fuzz_test.go`:

```go
//go:build go1.18

package path_test

import (
	"strings"
	"testing"
)

// FuzzChangePathContainment asserts the boundary SECURITY.md publishes: an
// accepted change name never resolves outside the managed root. Rejection is
// always an acceptable outcome; acceptance-with-escape never is.
func FuzzChangePathContainment(f *testing.F) {
	for _, seed := range []string{
		"safe-create", "", ".", "..", "../escape", "/abs", `C:\abs`,
		"a/../../b", "con", "nul", "COM1", ".specd", "archive",
		"a\x00b", "a\nb", " leading", "trailing ", strings.Repeat("a", 4096),
		"ünïcode", "a/b", `a\b`, "-flag", "--root",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		// Resolve `name` through the real resolver against a temp root.
		// If it is refused: assert the refusal names one legal next action.
		// If it is accepted: assert the resolved path is inside the root
		// after symlink evaluation and lexical cleaning, on this platform.
	})
}
```

Fill the body against the real `corepath` API. The seed corpus above is the
value of this target — it encodes the escapes worth remembering. Add
`internal/core/path/testdata/fuzz/FuzzChangePathContainment/` entries for any
crash found, and commit them: a found crash becomes a permanent regression test.

### 6.2 Record codec — torn appends

`internal/core/record/record_fuzz_test.go`: feed arbitrary bytes to the ledger
decoder. Seeds should include a valid two-entry ledger, that ledger truncated at
every byte offset, one with an embedded NUL, one with a trailing partial line,
one with CRLF line endings (`.gitattributes` pins this in the tree, but a
ledger written on Windows is a real input), and one with a future schema
version. Assert: no panic; a truncated tail is either replayed as the prefix of
valid entries or refused, never partially applied; a future schema version is
refused.

### 6.3 Plan parsers

`internal/plan/parse_fuzz_test.go`, one target per parser. Seed from the
existing `internal/plan/testdata/` corpus — those files are already the
interesting shapes. Assert no panic, one legal next action on refusal, and
round-trip stability for inputs that parse.

### 6.4 State decode

`internal/core/state/state_fuzz_test.go`: assert that a corrupt or future
`state.json` refuses and never yields a usable-looking zero value. This is the
fail-closed invariant, fuzzed.

### 6.5 Property test the transition relation

Not a fuzz target — a generated-sequence test using `math/rand` with a fixed
seed, in `internal/core`. Generate random sequences of task-activity transitions
and assert that the production transition function accepts exactly the sequences
the legal-transition table permits. This is the on-ramp to
[adoption 09](09-model-conformance.md); write the table here as test-local data,
because 09 depends on that table existing independently of production code.

### 6.6 Wire fuzzing into CI

Fuzz targets run as ordinary unit tests over their seed corpus under
`go test ./...` — that alone is worth having, and it needs no CI change.
Additionally run a short fuzzing burst so new inputs are actually explored:

```yaml
      # Seed corpora run as ordinary tests in the gates job. This step explores
      # beyond the corpus for a bounded time. It is not a proof of absence;
      # `go test -fuzz` runs one target at a time by design, so this rotates
      # through them rather than covering all of them every run.
      - name: fuzz burst
        run: |
          for target in FuzzChangePathContainment FuzzRecordLedger FuzzParseTasks; do
            go test ./... -run '^$' -fuzz "^${target}$" -fuzztime 60s
          done
```

Put it in the `repo` job (ubuntu only). Fuzzing four platforms multiplies cost
for little added signal — and say so in the comment, per P14.

## Acceptance

```bash
go test ./... -count=1            # seed corpora pass as normal tests
go test ./internal/core/path -run '^$' -fuzz FuzzChangePathContainment -fuzztime 120s
```

Then prove the target bites: temporarily remove one containment check from
`internal/core/path` and confirm the fuzzer finds an escaping input within the
budget. Revert, and record the finding in the acceptance note.

## Do not

- Do not add `gopter`, `rapid`, or any property-testing library. `testing.F`
  and `math/rand` with a fixed seed cover this.
- Do not assert only "no error." A parser that accepts everything passes that
  test.
- Do not let fuzzing write into a real `.specd/` root. Every target uses
  `t.TempDir()`.
- Do not delete a committed crash corpus entry to make the suite green. It is a
  regression test now.
- Do not run unbounded `-fuzztime` in CI. A gate that sometimes takes an hour is
  a gate people learn to skip.

## Deferred

Continuous fuzzing (OSS-Fuzz or a scheduled long-run workflow). Revisit once the
corpora have stabilized and the 60-second bursts stop finding anything — and
record that date, the way `release-decision.md` records every other observation.
