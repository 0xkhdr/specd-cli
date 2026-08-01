package plan

import (
	"bytes"
	"reflect"
	"testing"
)

func TestTaskValidBranchingPreservesBytesAndWaves(t *testing.T) {
	raw := []byte("# Tasks\r\n\r\n| id | role | files | depends-on | refs | verify | acceptance |\r\n|---|---|---|---|---|---|---|\r\n| T1 | builder | internal/a.go; internal/a_test.go | | api/Requirement: Parse tasks | `go test ./internal/plan -run 'TestA|TestB'` | parser test passes |\r\n| T2 | builder | internal/b.go | T1 | api/Requirement: Parse tasks | go test ./internal/plan | branch b passes |\r\n| T3 | builder | internal/c.go | T1 | api/Requirement: Parse tasks | go test ./internal/plan | branch c passes |\r\n")
	got := ParseTasks(Source{Path: "tasks.md", Bytes: raw, Present: true})
	if len(got.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", got.Diagnostics)
	}
	if !bytes.Equal(got.Source.Bytes, raw) || !bytes.Contains(got.Tasks[0].Source, []byte("'TestA|TestB'")) {
		t.Fatal("authored bytes changed")
	}
	if got.Tasks[0].Location.Line != 5 || got.Tasks[0].Verify != "go test ./internal/plan -run 'TestA|TestB'" {
		t.Fatalf("first task = %#v", got.Tasks[0])
	}
	want := []TaskWave{{Number: 0, Tasks: []string{"T1"}}, {Number: 1, Tasks: []string{"T2", "T3"}}}
	if !reflect.DeepEqual(got.Waves, want) {
		t.Fatalf("waves = %#v, want %#v", got.Waves, want)
	}
}

func TestTaskEscapedListsAndReferenceSyntax(t *testing.T) {
	raw := taskTable("| T1 | builder | docs/a\\;b.md; docs/c.md | | api/Requirement: Literal\\; separator | go test ./... | files exist |")
	got := ParseTasks(Source{Path: "tasks.md", Bytes: raw, Present: true})
	if len(got.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", got.Diagnostics)
	}
	if !reflect.DeepEqual(got.Tasks[0].Files, []string{"docs/a;b.md", "docs/c.md"}) ||
		got.Tasks[0].References[0].Requirement != "Literal; separator" {
		t.Fatalf("task = %#v", got.Tasks[0])
	}
}

func TestTaskRejectsStructureFieldsAndPaths(t *testing.T) {
	cases := map[string]string{
		"missing":            "task_table_missing",
		"role":               "task_role",
		"empty_verify":       "task_verify_empty",
		"empty_refs":         "task_refs_empty",
		"bad_ref":            "task_ref_syntax",
		"absolute":           "task_file_unsafe",
		"traversal":          "task_file_unsafe",
		"glob":               "task_file_unsafe",
		"directory":          "task_file_unsafe",
		"placeholder":        "task_acceptance_placeholder",
		"placeholder_verify": "task_verify_placeholder",
		"bad_escape":         "task_list_escape",
	}
	inputs := map[string][]byte{
		"missing":            []byte("# Tasks\n"),
		"role":               taskTable("| T1 | reviewer | a.go | | api/Requirement: R | go test ./... | passes |"),
		"empty_verify":       taskTable("| T1 | builder | a.go | | api/Requirement: R | | passes |"),
		"empty_refs":         taskTable("| T1 | builder | a.go | | | go test ./... | passes |"),
		"bad_ref":            taskTable("| T1 | builder | a.go | | R1 | go test ./... | passes |"),
		"absolute":           taskTable("| T1 | builder | /a.go | | api/Requirement: R | go test ./... | passes |"),
		"traversal":          taskTable("| T1 | builder | ../a.go | | api/Requirement: R | go test ./... | passes |"),
		"glob":               taskTable("| T1 | builder | *.go | | api/Requirement: R | go test ./... | passes |"),
		"directory":          taskTable("| T1 | builder | internal/ | | api/Requirement: R | go test ./... | passes |"),
		"placeholder":        taskTable("| T1 | builder | a.go | | api/Requirement: R | go test ./... | TODO |"),
		"placeholder_verify": taskTable("| T1 | builder | a.go | | api/Requirement: R | `<verification>` | passes |"),
		"bad_escape":         taskTable("| T1 | builder | a\\q.go | | api/Requirement: R | go test ./... | passes |"),
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			got := ParseTasks(Source{Path: "tasks.md", Bytes: inputs[name], Present: true})
			if !hasCode(got.Diagnostics, code) {
				t.Fatalf("missing %s: %#v", code, got.Diagnostics)
			}
			if name != "missing" && (len(got.Tasks) != 1 || got.Tasks[0].Valid || len(got.Tasks[0].Files) != 0) {
				t.Fatalf("refused row exposed executable scope: %#v", got.Tasks)
			}
		})
	}
}

func TestTaskMalformedRowDoesNotHideLaterRows(t *testing.T) {
	raw := taskTable("| broken row\n" + validRow("T2", ""))
	got := ParseTasks(Source{Path: "tasks.md", Bytes: raw, Present: true})
	if !hasCode(got.Diagnostics, "task_row_malformed") || len(got.Tasks) != 1 || got.Tasks[0].ID != "T2" || !got.Tasks[0].Valid {
		t.Fatalf("result = %#v", got)
	}
}

func TestTaskRejectsUnstableIDWithoutDependencyCascade(t *testing.T) {
	raw := taskTable("| bad id | builder | bad.go | | api/Requirement: R | go test ./... | concrete result |\n" +
		"| T2 | builder | good.go | bad id | api/Requirement: R | go test ./... | concrete result |")
	got := ParseTasks(Source{Path: "tasks.md", Bytes: raw, Present: true})
	if !hasCode(got.Diagnostics, "task_id_syntax") || hasCode(got.Diagnostics, "task_dependency_unknown") ||
		len(got.Tasks) != 2 || got.Tasks[0].Valid || got.Tasks[1].Valid ||
		len(got.Tasks[0].Files) != 0 || len(got.Tasks[1].Files) != 0 {
		t.Fatalf("result = %#v", got)
	}
}

func TestTaskRequiresExactHeader(t *testing.T) {
	raw := []byte("| ID | role | files | depends-on | refs | verify | acceptance |\n|---|---|---|---|---|---|---|\n" + validRow("T1", "") + "\n")
	got := ParseTasks(Source{Path: "tasks.md", Bytes: raw, Present: true})
	if !hasCode(got.Diagnostics, "task_table_missing") {
		t.Fatalf("result = %#v", got)
	}
}

func TestTaskRejectsIDsDependenciesAndCycle(t *testing.T) {
	tests := []struct {
		name, rows, code string
	}{
		{"duplicate", validRow("T1", "") + "\n" + validRow("T1", ""), "task_id_duplicate"},
		{"unknown", validRow("T1", "T9"), "task_dependency_unknown"},
		{"self", validRow("T1", "T1"), "task_dependency_self"},
		{"cycle", validRow("T1", "T2") + "\n" + validRow("T2", "T1"), "task_dependency_cycle"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseTasks(Source{Path: "tasks.md", Bytes: taskTable(test.rows), Present: true})
			if !hasCode(got.Diagnostics, test.code) || len(got.Waves) != 0 {
				t.Fatalf("result = %#v", got)
			}
		})
	}
}

func TestTaskLFCRLFParityAndMissing(t *testing.T) {
	lf := taskTable(validRow("T1", ""))
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	a := ParseTasks(Source{Path: "tasks.md", Bytes: lf, Present: true})
	b := ParseTasks(Source{Path: "tasks.md", Bytes: crlf, Present: true})
	if !reflect.DeepEqual(codes(a.Diagnostics), codes(b.Diagnostics)) ||
		!reflect.DeepEqual(a.Waves, b.Waves) || bytes.Equal(a.Source.Bytes, b.Source.Bytes) {
		t.Fatalf("LF/CRLF drift: %#v %#v", a, b)
	}
	missing := ParseTasks(Source{Path: "tasks.md"})
	if !hasCode(missing.Diagnostics, "artifact_missing") {
		t.Fatalf("missing = %#v", missing)
	}
}

func taskTable(rows string) []byte {
	return []byte("| id | role | files | depends-on | refs | verify | acceptance |\n|---|---|---|---|---|---|---|\n" + rows + "\n")
}

func validRow(id, dependency string) string {
	return "| " + id + " | builder | internal/" + id + ".go | " + dependency +
		" | api/Requirement: Parse tasks | go test ./internal/plan | passes |"
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, item := range diagnostics {
		if item.Code == code {
			return true
		}
	}
	return false
}
