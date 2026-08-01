package core

import (
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestTaskGraphExactTopologyAndWaves(t *testing.T) {
	tests := []struct {
		name  string
		tasks []plan.Task
		order []string
		waves [][]string
	}{
		{name: "empty", order: []string{}, waves: [][]string{}},
		{
			name:  "chain",
			tasks: []plan.Task{task("A"), task("B", "A"), task("C", "B")},
			order: []string{"A", "B", "C"},
			waves: [][]string{{"A"}, {"B"}, {"C"}},
		},
		{
			name: "diamond",
			tasks: []plan.Task{
				task("A"), task("B", "A"), task("C", "A"), task("D", "B", "C"),
			},
			order: []string{"A", "B", "C", "D"},
			waves: [][]string{{"A"}, {"B", "C"}, {"D"}},
		},
		{
			name:  "independent authored order",
			tasks: []plan.Task{task("Z"), task("A"), task("M")},
			order: []string{"Z", "A", "M"},
			waves: [][]string{{"Z", "A", "M"}},
		},
		{
			name: "newly eligible earlier task",
			tasks: []plan.Task{
				task("child", "root"), task("root"), task("later"),
			},
			order: []string{"root", "child", "later"},
			waves: [][]string{{"root", "later"}, {"child"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, err := BuildTaskGraph(plan.Tasks{Tasks: test.tasks})
			if err != nil {
				t.Fatal(err)
			}
			if got := graph.TopologicalOrder(); !reflect.DeepEqual(got, test.order) {
				t.Fatalf("order = %#v, want %#v", got, test.order)
			}
			if got := graph.Waves(); !reflect.DeepEqual(got, test.waves) {
				t.Fatalf("waves = %#v, want %#v", got, test.waves)
			}
			if again := graph.TopologicalOrder(); !reflect.DeepEqual(again, test.order) {
				t.Fatalf("repeat order = %#v, want %#v", again, test.order)
			}
		})
	}
}

func TestTaskGraphEdgesAndImmutableQueries(t *testing.T) {
	graph, err := BuildTaskGraph(plan.Tasks{Tasks: []plan.Task{
		task("A"), task("B", "A"), task("C", "A"), task("D", "B", "C"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := graph.Dependencies("D"), []string{"B", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %#v, want %#v", got, want)
	}
	if got, want := graph.Dependents("A"), []string{"B", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents = %#v, want %#v", got, want)
	}
	if wave, exists := graph.Wave("D"); !exists || wave != 2 {
		t.Fatalf("D wave = %d, %v", wave, exists)
	}
	graph.TopologicalOrder()[0] = "changed"
	graph.Waves()[0][0] = "changed"
	graph.Dependencies("D")[0] = "changed"
	if graph.TopologicalOrder()[0] != "A" || graph.Waves()[0][0] != "A" ||
		graph.Dependencies("D")[0] != "B" {
		t.Fatal("query result mutated graph")
	}
}

func TestTaskGraphRejectsInvalidWithoutPartialGraph(t *testing.T) {
	tests := []struct {
		name string
		rows []plan.Task
		ids  []string
	}{
		{name: "empty id", rows: []plan.Task{task("")}, ids: []string{"position 1"}},
		{name: "duplicate", rows: []plan.Task{task("A"), task("A")}, ids: []string{"A"}},
		{name: "missing", rows: []plan.Task{task("A", "missing")}, ids: []string{"A", "missing"}},
		{name: "self", rows: []plan.Task{task("A", "A")}, ids: []string{"A"}},
		{
			name: "cycle",
			rows: []plan.Task{task("A", "B"), task("B", "C"), task("C", "A"), task("D", "C")},
			ids:  []string{"A", "B", "C"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, err := BuildTaskGraph(plan.Tasks{
				Source: plan.Source{Path: "tasks.md"}, Tasks: test.rows,
			})
			if err == nil || !failure.IsCode(err, "plan_invalid") {
				t.Fatalf("error = %v", err)
			}
			refusal, ok := err.(*TaskGraphRefusal)
			if !ok || refusal.Owner != "author" {
				t.Fatalf("refusal = %#v, want author-owned TaskGraphRefusal", err)
			}
			if len(graph.TopologicalOrder()) != 0 || len(graph.Waves()) != 0 {
				t.Fatalf("partial graph exposed: %#v", graph)
			}
			for _, id := range test.ids {
				if !strings.Contains(err.Error(), id) {
					t.Fatalf("error %q does not name %q", err, id)
				}
			}
			if !strings.Contains(err.Error(), "edit tasks.md and run check") {
				t.Fatalf("non-actionable error: %v", err)
			}
		})
	}
}

func TestTaskGraphRejectsCanonicalParserDiagnostics(t *testing.T) {
	raw := []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n" +
			"|---|---|---|---|---|---|---|\n" +
			"| A | reviewer | internal/a.go | | sample/Requirement: A | `go test ./...` | A works |\n",
	)
	tasks := plan.ParseTasks(plan.Source{Path: "tasks.md", Bytes: raw, Present: true})
	if len(tasks.Diagnostics) == 0 || len(tasks.Tasks) != 1 {
		t.Fatalf("invalid parser fixture = %#v", tasks)
	}
	graph, err := BuildTaskGraph(tasks)
	if err == nil || !failure.IsCode(err, "plan_invalid") {
		t.Fatalf("error = %v, want plan_invalid", err)
	}
	if len(graph.AuthoredOrder()) != 0 || len(graph.TopologicalOrder()) != 0 ||
		len(graph.Waves()) != 0 {
		t.Fatalf("invalid canonical model exposed graph: %#v", graph)
	}
	if refusal, ok := err.(*TaskGraphRefusal); !ok || refusal.Owner != "author" {
		t.Fatalf("refusal = %#v, want author owner", err)
	}
}

func task(id string, dependencies ...string) plan.Task {
	return plan.Task{ID: id, DependsOn: dependencies, Valid: true}
}
