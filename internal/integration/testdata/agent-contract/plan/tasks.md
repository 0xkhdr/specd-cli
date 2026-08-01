| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| edit-sample | builder | sample.go | | sample/Requirement: Current greeting | `go test . -run '^TestSampleLoop$' -count=1` | The greeting is current and its test passes |
