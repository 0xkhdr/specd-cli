# Scale observations

These are dated measurements, not supported limits. They describe one machine
and the shapes that ran; they do not predict another host or a long-lived
change.

## Method

`go test ./... -run '^$' -bench . -benchmem -benchtime 3x` on a 12th Gen Intel
Core i7-1255U, Linux 7.0.0-28-generic amd64, Go 1.26.4, 2026-08-06. Ledger
records were valid independent creation records, task graphs contained
independent tasks, state documents carried one activity per task, and context
files contained 18 bytes each.

## Observations

| operation | size | ns/op | B/op | allocs/op | observed growth |
| --- | ---: | ---: | ---: | ---: | --- |
| ledger replay | 100 / 1k / 10k / 100k records | 2.14m / 19.0m / 164m / 1.49b | 1.18m / 11.9m / 119m / 1.17b | 23k / 231k / 2.31m / 23.1m | approximately linear; 100k took 1.49s and 1.17GB allocated |
| state read-modify-write | 10 / 100 / 1k / 10k tasks | 898k / 984k / 3.62m / 23.7m | 17k / 81k / 862k / 8.87m | 361 / 2.08k / 19.2k / 190k | approximately linear above filesystem noise |
| readiness projection | 10 / 100 / 1k / 10k tasks | 36.8k / 135k / 3.88m / 364m | 13k / 115k / 1.32m / 13.0m | 115 / 724 / 7.00k / 70.2k | superlinear; 10k independent tasks is the observed wall |
| context assembly | 10 / 100 / 1k files | 348k / 3.18m / 18.5m | 63k / 608k / 5.99m | 561 / 5.39k / 53.9k | approximately linear |

## Published boundary

The paths were measured through 10,000 tasks, 1,000 context files, and 100,000
ledger records on this machine. At 10,000 tasks, readiness projection took
about 364ms; at 100,000 records, ledger replay took about 1.49s and allocated
about 1.17GB. specd publishes no supported maximum from one host observation.

## Not measured

Concurrent callers under load, archive growth over months, a very large working
tree, Windows and macOS filesystem behavior, and a change kept open across
weeks or hundreds of commits. The next ratchet is a long-lived-change soak.
