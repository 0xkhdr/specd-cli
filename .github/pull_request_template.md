## What changed

<!-- The behavior, not the diff. -->

## How you verified it

<!-- The commands you actually ran, and what they printed. -->

```
go test ./... -race -count=1
go vet ./...
gofmt -l .
```

## Related work

<!-- Link the closest open issue or pull request, or write "none found". -->

## Checklist

- [ ] I searched open issues and pull requests and linked the closest result above
- [ ] I opened an issue first if this is more than a small fix
- [ ] I reviewed and understand the final change, including agent-authored work
- [ ] `go test ./... -race -count=1` passes
- [ ] `go vet ./...` is clean and `gofmt -l .` prints nothing
- [ ] No new dependency — `go.mod` still has an empty require set
- [ ] New exported surface has an owner in `release/surface-inventory.md`
- [ ] `docs/operations.md` regenerated, if the operation registry changed
- [ ] `release/release-decision.md` updated, if this changes what is claimed or proven
- [ ] No bypass added for evidence, approval, authority, scope, or validation
