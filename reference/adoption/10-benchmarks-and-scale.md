# 10 — Turn the scale disclaimer into a number

| Pattern | Phase | Effort | Risk | Status |
| --- | --- | --- | --- | --- |
| [P9](../patterns.md#p9--a-capacity-claim-needs-an-executable-model) | 3 | medium | low | applied 2026-08-06 |

## Why

buzz ships `perf/RELAY_BUS_SCALING.md` next to `relay_bus_scaling.py` and
`test_relay_bus_scaling.py` — the capacity model is executable, and the model
itself is tested.

specd currently states, in `README.md` and `release/release-decision.md`, that
"scale and long-lived changes are not yet proven." That sentence is honest and
correct today. It is also the only claim in the project with no path to
resolution: every other unproven thing has a named gate that would prove it.
There are **zero** `func Benchmark` in the repository.

The costs are real and knowable. specd appends to `history.jsonl` and
`evidence.jsonl` on every state write and replays them; a long-lived change with
hundreds of tasks and thousands of ledger entries is the growth curve that
matters. Nobody has measured whether replay is linear, whether readiness
projection re-reads the task graph per call, or where the first wall is.

## What to measure

Pick the four operations whose cost grows with the change, not with the
repository:

| Benchmark | Grows with | Why it matters |
| --- | --- | --- |
| ledger replay (`internal/core/record`) | number of history + evidence entries | every state read pays it |
| state read-modify-write (`internal/core/persist`, `state`) | state document size | every state-writing operation pays it |
| readiness / frontier projection (`internal/core/readiness`, `taskgraph`) | number of tasks and dependency edges | `next` and `status` pay it |
| bounded context assembly (`internal/context`) | declared file count and byte budget | `context` pays it, and it has a stated budget |

## Change set

### 10.1 Benchmarks

Standard `testing.B`, no dependency, sized by a table so the growth curve is
visible rather than a single point:

```go
func BenchmarkLedgerReplay(b *testing.B) {
	for _, entries := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			root := b.TempDir()
			// seed a ledger with `entries` records
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// replay
			}
		})
	}
}
```

Sizes chosen so the curve is readable: if 100k entries costs ~1000× the 100-entry
case, replay is linear and the answer is a per-entry constant. If it is worse
than linear, that is the finding, and it is a defect report rather than a
benchmark result.

### 10.2 The scale note

Create `release/scale.md`. Keep it in the register of
`release/release-decision.md` — measured, dated, bounded:

```markdown
# Scale

What has been measured, on what, when. Nothing here is a supported limit; it is
an observation, and a limit specd has not observed is a limit specd does not
claim. `release/gate-limits.md` applies: these numbers describe the machine and
the shapes that ran, not a guarantee.

## Method

`go test ./... -bench . -benchmem -run '^$'`, on <cpu/os>, Go <version>,
<date>. Ledger entries are <shape>; task graphs are <shape>.

## Observations

| operation | size | ns/op | B/op | allocs/op | growth |
| --- | --- | --- | --- | --- | --- |
| ledger replay | 100 / 1k / 10k / 100k | … | … | … | linear |
| state write | … | … | … | … | … |
| readiness projection | 10 / 100 / 1k tasks | … | … | … | … |
| context assembly | … | … | … | … | … |

## What this changes about the published claim

<Either: the disclaimer narrows to the shapes not measured — or: a wall was
found at <size>, and here is the refusal or degradation a caller sees at it.>

## Not measured

Concurrent callers under load (contention is proven by
`internal/integration/concurrency_test.go` but not measured); archive growth
over months; a repository with a very large working tree; Windows and macOS
filesystem behavior, which is where the atomic-write path differs most.
```

### 10.3 Update the disclaimer

Once numbers exist, edit `README.md` §Project status and
`release/release-decision.md` so the sentence points at the measurement and
narrows to what is genuinely unmeasured. Do not delete the disclaimer — narrow
it. Deleting it would claim more than a benchmark run establishes, which is the
failure mode `v0.1.0` was retracted for.

### 10.4 CI

Run benchmarks **once** per CI run, on ubuntu only, with `-benchtime 1x`:

```yaml
      # Benchmarks run for compilation and panic-freedom, not for timing:
      # shared runners give timings too noisy to gate on, and a flaky
      # performance gate is a gate people learn to ignore. The numbers in
      # release/scale.md come from a recorded manual run on named hardware.
      - name: benchmarks compile and run
        run: go test ./... -run '^$' -bench . -benchtime 1x
```

## Acceptance

```bash
go test ./... -run '^$' -bench . -benchmem     # all four benchmarks report
```

`release/scale.md` exists with real numbers, a named machine, and a date.
`README.md`'s status paragraph either narrows or cites the file. If a wall was
found, it is described in terms of what a caller *observes* — a refusal, a
timeout, a slow command — not just a number.

## Do not

- **Do not gate CI on timing thresholds.** Shared runners vary by 2–3×.
- **Do not add a benchmarking framework.** `testing.B` is complete for this.
- **Do not publish a supported limit.** specd's convention is that a claim is
  earned by an observation; a benchmark observes *this machine*, so the document
  says so.
- **Do not benchmark against a real `.specd/` root** in the repository.
  `b.TempDir()`.
- **Do not optimize on the first result.** Record it, then decide. `AGENTS.md`:
  complexity must earn its place through real use.

## Deferred

A long-lived-change soak (drive one change through hundreds of tasks over
simulated weeks). It is the real answer to "long-lived changes are not yet
proven" and it is a bigger item than benchmarks; name it as the next ratchet in
`release/scale.md`.

## Acceptance note — 2026-08-06

`go test ./... -run '^$' -bench . -benchmem -benchtime 1x` reports all four
families — context assembly, readiness projection, state read-modify-write, and
ledger replay — with no panic. `release/scale.md` carries the `-benchtime 3x`
numbers, the named machine, and the date, and `README.md`'s status paragraph
cites it rather than standing alone. The readiness wall at 10,000 tasks is
stated as what a caller observes: a projection that takes about a third of a
second before any command output appears.
