| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| T1 | builder | release/tag-contract.sh | | release/Requirement: Tag contract precedes release work | `sh release/tag-contract.sh --self-check` | The script refuses a lightweight tag and a version with no changelog section, accepts a tag meeting both clauses, and says which clause it refused. |
| T2 | builder | .github/workflows/release.yml | T1 | release/Requirement: Tag contract precedes release work | `sh release/tag-contract.sh v0.2.0` | A contract job runs the script on the pushed tag, and gates depends on it, so nothing builds before the contract holds. |
| T3 | builder | release/release-decision.md; TODO.md; CHANGELOG.md | T2 | release/Requirement: Tag contract precedes release work | `go test ./internal/integration -count=1` | The records say the release machinery has been through the loop and no longer name it as the part that has not. |
