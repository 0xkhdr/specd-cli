| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| S2-001 | builder | internal/accounts/update.go | | accounts/Requirement: Stable locking | `go test ./internal/accounts` | Concurrent update check passes |
