## What changed

<!-- The behavior, not the diff. -->

## How you verified it

<!-- The commands you actually ran, and what they printed. -->

```
go test ./... -race -count=1
go vet ./...
gofmt -l .
```

## Checklist

- [ ] `go test ./... -race -count=1` passes
- [ ] `go vet ./...` is clean and `gofmt -l .` prints nothing
- [ ] No new dependency — `go.mod` still has an empty require set
- [ ] New exported surface has an owner in `release/surface-inventory.md`
- [ ] `docs/operations.md` regenerated, if the operation registry changed
- [ ] `release/release-decision.md` updated, if this changes what is claimed or proven
- [ ] No bypass added for evidence, approval, authority, scope, or validation
