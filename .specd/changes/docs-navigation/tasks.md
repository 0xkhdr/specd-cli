| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| T1 | builder | README.md; docs/README.md | | documentation/Requirement: Audience-oriented entry points; documentation/Requirement: Single ownership of documentation navigation; documentation/Requirement: Existing claims remain linked and verifiable | `go test ./internal/integration -run TestReleaseQualification -count=1` | The root README is a concise landing page, docs/README.md routes readers by goal and order, all referenced files exist, and release qualification passes |
