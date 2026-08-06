# The local projection of .github/workflows/ci.yml. CI remains the authority:
# it calls the underlying commands directly so a reader of the workflow never
# has to resolve a Makefile, and so the Windows leg needs no make at all.
# If a gate is added to the workflow, it is added here in the same commit.

.PHONY: ci check test vet fmt fmt-check deps-empty vuln smoke docs hooks

ci: check test smoke

check: fmt-check vet deps-empty vuln

test:
	go test ./... -race -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	  test -z "$$unformatted" || { echo "$$unformatted"; exit 1; }

# A failing `go list` prints nothing, and nothing reads as "no dependencies",
# so its exit status is checked before its output is.
deps-empty:
	@modules="$$(go list -m all)" \
	  || { echo "go list -m all failed; the require set could not be read"; exit 1; }; \
	  test -z "$$(printf '%s\n' "$$modules" | tail -n +2)" \
	  || { echo "specd is standard library only; a dependency was added"; exit 1; }

# Advisories reachable from code specd actually calls. Needs network by design:
# a scanner reading a frozen advisory database is the one thing it must not be.
# Run `make check-offline` on a host without network.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# `check` without the one gate that requires network.
.PHONY: check-offline
check-offline: fmt-check vet deps-empty

smoke:
	go run ./cmd/specd --version
	go run ./cmd/specd --help

# Regenerate the operations document after an operation-registry change.
docs:
	SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity

# Install the repository's git hooks. Separate, consented action: no other
# target rewrites a contributor's git config.
hooks:
	git config core.hooksPath .githooks
