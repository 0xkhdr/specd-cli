| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| T1 | builder | docs/layout.md;docs/troubleshooting.md | | release-assurance/Requirement: Managed-name documentation parity | `go test ./internal/core/path ./internal/integration -count=1` | Naming and refusal documentation state that Windows device names are reserved on every platform, while generated docs and behavior remain unchanged. |
