package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

// TaskGraph is a pure projection of canonical tasks. Query methods return
// copies so consumers cannot mutate shared graph truth.
type TaskGraph struct {
	authored     []string
	order        []string
	waves        [][]string
	dependencies map[string][]string
	dependents   map[string][]string
	wave         map[string]int
}

type TaskGraphRefusal struct {
	*failure.Refusal
	Owner string
}

func (r *TaskGraphRefusal) Unwrap() error { return r.Refusal }

func BuildTaskGraph(tasks plan.Tasks) (TaskGraph, error) {
	if len(tasks.Diagnostics) != 0 {
		diagnostic := tasks.Diagnostics[0]
		return TaskGraph{}, invalidTaskGraph(tasks.Source.Path,
			"canonical task plan is invalid: %s: %s", diagnostic.Code, diagnostic.Message)
	}
	byID := make(map[string]int, len(tasks.Tasks))
	for i, task := range tasks.Tasks {
		if task.ID == "" {
			return TaskGraph{}, invalidTaskGraph(tasks.Source.Path, "task at authored position %d has empty id", i+1)
		}
		if !task.Valid {
			return TaskGraph{}, invalidTaskGraph(tasks.Source.Path, "task %q is invalid", task.ID)
		}
		if _, exists := byID[task.ID]; exists {
			return TaskGraph{}, invalidTaskGraph(tasks.Source.Path, "duplicate task id %q", task.ID)
		}
		byID[task.ID] = i
	}

	graph := TaskGraph{
		authored:     make([]string, len(tasks.Tasks)),
		dependencies: make(map[string][]string, len(tasks.Tasks)),
		dependents:   make(map[string][]string, len(tasks.Tasks)),
		wave:         make(map[string]int, len(tasks.Tasks)),
	}
	for i, task := range tasks.Tasks {
		graph.authored[i] = task.ID
		graph.dependencies[task.ID] = slices.Clone(task.DependsOn)
		graph.dependents[task.ID] = nil
	}
	for _, task := range tasks.Tasks {
		for _, dependency := range task.DependsOn {
			if dependency == task.ID {
				return TaskGraph{}, invalidTaskGraph(tasks.Source.Path, "task %q depends on itself", task.ID)
			}
			if _, exists := byID[dependency]; !exists {
				return TaskGraph{}, invalidTaskGraph(tasks.Source.Path,
					"task %q depends on missing task %q", task.ID, dependency)
			}
			graph.dependents[dependency] = append(graph.dependents[dependency], task.ID)
		}
	}

	indegree := make(map[string]int, len(tasks.Tasks))
	for _, task := range tasks.Tasks {
		indegree[task.ID] = len(task.DependsOn)
	}
	for len(graph.order) < len(tasks.Tasks) {
		next := ""
		for _, id := range graph.authored {
			if indegree[id] == 0 {
				next = id
				break
			}
		}
		if next == "" {
			return TaskGraph{}, invalidTaskGraph(tasks.Source.Path,
				"task dependency cycle involving %s", strings.Join(cyclicTaskIDs(graph.authored, graph.dependencies), ", "))
		}
		indegree[next] = -1
		graph.order = append(graph.order, next)
		for _, dependent := range graph.dependents[next] {
			indegree[dependent]--
		}
	}

	for _, id := range graph.order {
		wave := 0
		for _, dependency := range graph.dependencies[id] {
			if candidate := graph.wave[dependency] + 1; candidate > wave {
				wave = candidate
			}
		}
		graph.wave[id] = wave
		for len(graph.waves) <= wave {
			graph.waves = append(graph.waves, nil)
		}
		graph.waves[wave] = append(graph.waves[wave], id)
	}
	return graph, nil
}

func (g TaskGraph) AuthoredOrder() []string { return append([]string{}, g.authored...) }
func (g TaskGraph) TopologicalOrder() []string {
	return append([]string{}, g.order...)
}
func (g TaskGraph) Waves() [][]string {
	waves := make([][]string, len(g.waves))
	for i := range g.waves {
		waves[i] = slices.Clone(g.waves[i])
	}
	return waves
}
func (g TaskGraph) Dependencies(id string) []string {
	return slices.Clone(g.dependencies[id])
}
func (g TaskGraph) Dependents(id string) []string {
	return slices.Clone(g.dependents[id])
}
func (g TaskGraph) Wave(id string) (int, bool) {
	wave, exists := g.wave[id]
	return wave, exists
}

func invalidTaskGraph(path, format string, args ...any) error {
	return &TaskGraphRefusal{
		Refusal: failure.New("plan_invalid", "", path, fmt.Sprintf(format, args...),
			"edit tasks.md and run check"),
		Owner: "author",
	}
}

func cyclicTaskIDs(authored []string, dependencies map[string][]string) []string {
	state := make(map[string]uint8, len(authored))
	stack := make([]string, 0, len(authored))
	var cycle []string
	var visit func(string) bool
	visit = func(id string) bool {
		state[id] = 1
		stack = append(stack, id)
		for _, dependency := range dependencies[id] {
			if state[dependency] == 1 {
				start := 0
				for stack[start] != dependency {
					start++
				}
				cycle = append([]string(nil), stack[start:]...)
				return true
			}
			if state[dependency] == 0 && visit(dependency) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return false
	}
	for _, id := range authored {
		if state[id] == 0 && visit(id) {
			return cycle
		}
	}
	return nil
}
