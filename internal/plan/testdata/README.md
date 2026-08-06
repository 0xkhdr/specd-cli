# Plan fixtures

Directories contain authored parser inputs. Generated expected results use
`good_<case>.expected.json`; adversarial inputs use
`bad_<invariant>_<violation>`.

Expected bytes are review contracts. Refresh them explicitly with:

```bash
SPECD_WRITE_PLAN_FIXTURES=1 go test ./internal/plan -run TestPlanGoldenByteContract
```

Refreshing exposes a changed result; it does not prove that result correct.
Every fixture must be read by a named test.
