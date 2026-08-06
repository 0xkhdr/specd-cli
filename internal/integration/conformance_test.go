package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
)

// conformanceModel is deliberately decoded directly from persisted JSON. It
// does not call production lifecycle, activity, readiness, or evidence logic.
type conformanceModel struct {
	Change       string              `json:"change,omitempty"`
	Exists       bool                `json:"exists"`
	Lifecycle    string              `json:"lifecycle,omitempty"`
	Revision     uint64              `json:"revision,omitempty"`
	Tasks        map[string]string   `json:"tasks,omitempty"`
	Evidence     map[string]string   `json:"evidence,omitempty"`
	Dependencies map[string][]string `json:"dependencies,omitempty"`
}

type conformanceAttempt struct {
	ID           string `json:"id"`
	BaselineHEAD string `json:"baseline_head"`
	ApprovalHash string `json:"approval_hash"`
}

type conformanceStep struct {
	Schema     int              `json:"schema"`
	Journey    string           `json:"journey"`
	Operation  string           `json:"operation"`
	Actor      string           `json:"actor"`
	Task       string           `json:"task,omitempty"`
	Exit       int              `json:"exit"`
	Before     conformanceModel `json:"before"`
	After      conformanceModel `json:"after"`
	Refusal    string           `json:"refusal,omitempty"`
	NextAction string           `json:"next_action,omitempty"`
	ImplBug    string           `json:"impl_bug,omitempty"`
}

type conformanceCoverage struct {
	Operations []string
	Journeys   []string
	Refusal    bool
}

type conformanceRule struct {
	actors     string
	lifecycles string
	kind       string
}

// conformanceRules restates the executable operation contract without using
// production transition, readiness, or evidence helpers. A registry operation
// missing here is a coverage breach, not a permissive default.
var conformanceRules = map[string]conformanceRule{
	"init":     {"agent,human", "", "unchanged"},
	"new":      {"agent,human", "", "new"},
	"check":    {"agent,human", "planning,approved,executing,reconciling,archived", "unchanged"},
	"approve":  {"human", "planning", "approve"},
	"reopen":   {"agent,human", "approved", "reopen"},
	"status":   {"agent,human", "planning,approved,executing,reconciling,archived", "unchanged"},
	"next":     {"agent,human", "planning,approved,executing,reconciling,archived", "unchanged"},
	"context":  {"agent,human", "approved", "unchanged"},
	"start":    {"agent,human", "approved", "start"},
	"verify":   {"agent,human", "approved", "verify"},
	"complete": {"agent,human", "approved", "complete"},
	"review":   {"agent,human", "approved", "unchanged"},
	"sync":     {"human", "approved,reconciling", "sync"},
	"archive":  {"agent,human", "reconciling", "archive"},
	"report":   {"agent,human", "planning,approved,executing,reconciling,archived", "unchanged"},
	"friction": {"agent,human", "planning,approved,executing,reconciling,archived", "unchanged"},
}

type traceCollector struct{ steps []conformanceStep }

var activeConformance *traceCollector

type pendingConformance struct {
	collector *traceCollector
	root      string
	change    string
	step      conformanceStep
}

func beginConformanceTrace(t *testing.T, root string, route cmd.Route, args []string) pendingConformance {
	if activeConformance == nil || len(args) == 0 {
		return pendingConformance{}
	}
	actor := "agent"
	if route == cmd.RouteHumanTerminal {
		actor = "human"
	}
	step := conformanceStep{Schema: 1, Journey: conformanceJourney(t.Name()), Operation: args[0], Actor: actor}
	change := conformanceChange(args)
	step.Task = conformanceTask(args)
	step.Before = readConformanceModel(root, change, "")
	return pendingConformance{collector: activeConformance, root: root, change: change, step: step}
}

func (pending pendingConformance) finish(code int, stdout string) {
	if pending.collector == nil {
		return
	}
	change := pending.change
	pending.step.Exit = code
	var document map[string]any
	jsonOutput := json.Unmarshal([]byte(stdout), &document) == nil
	textOutput := strings.Contains(stdout, "operation: "+pending.step.Operation+"\n") &&
		strings.Contains(stdout, "exit:")
	if !jsonOutput && !textOutput {
		pending.step.ImplBug = "operation produced no valid result envelope"
	}
	if jsonOutput {
		operation, _ := document["operation"].(string)
		if operation != pending.step.Operation {
			pending.step.ImplBug = "result envelope operation does not match invocation"
		}
	}
	if data, _ := document["data"].(map[string]any); data != nil {
		if change == "" {
			change, _ = data["change"].(string)
		}
		if pending.step.Operation == "archive" && code == 0 {
			if target, _ := data["target"].(string); target != "" {
				pending.step.After = readConformanceModelAt(filepath.Join(pending.root, ".specd", filepath.FromSlash(target), "state.json"), change)
			}
		}
	}
	if !pending.step.After.Exists {
		pending.step.After = readConformanceModel(pending.root, change, "")
	}
	if code != 0 {
		if code == 2 && document != nil {
			if diagnostics, _ := document["diagnostics"].([]any); len(diagnostics) > 0 {
				first, _ := diagnostics[0].(map[string]any)
				pending.step.Refusal, _ = first["code"].(string)
			}
			if next, _ := document["next"].(map[string]any); next != nil {
				pending.step.NextAction, _ = next["instruction"].(string)
			}
		}
	}
	pending.collector.steps = append(pending.collector.steps, pending.step)
}

func conformanceChange(args []string) string {
	if len(args) > 1 && args[0] != "init" {
		return args[1]
	}
	return ""
}

func conformanceTask(args []string) string {
	if len(args) > 2 && slices.Contains([]string{"next", "context", "start", "verify", "complete", "review", "friction"}, args[0]) {
		return args[2]
	}
	return ""
}

func conformanceJourney(name string) string {
	for _, part := range strings.Split(name, "/") {
		if len(part) >= 2 && part[0] >= '0' && part[0] <= '9' && part[1] >= '0' && part[1] <= '9' {
			return part[:2]
		}
	}
	return "setup"
}

func readConformanceModel(root, change, statePath string) conformanceModel {
	if change == "" {
		return conformanceModel{}
	}
	if statePath == "" {
		statePath = filepath.Join(root, ".specd", "changes", change, "state.json")
	}
	return readConformanceModelAt(statePath, change)
}

func readConformanceModelAt(path, change string) conformanceModel {
	raw, err := os.ReadFile(path)
	if err != nil {
		return conformanceModel{}
	}
	var state struct {
		Stage      string                     `json:"stage"`
		Revision   uint64                     `json:"revision"`
		Tasks      map[string]json.RawMessage `json:"tasks"`
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return conformanceModel{Change: change, Exists: true}
	}
	model := conformanceModel{Change: change, Exists: true, Lifecycle: state.Stage, Revision: state.Revision, Tasks: map[string]string{}}
	for id, raw := range state.Tasks {
		var activity string
		if json.Unmarshal(raw, &activity) == nil {
			model.Tasks[id] = activity
		}
	}
	model.Dependencies = readConformanceDependencies(filepath.Join(filepath.Dir(path), "tasks.md"))
	specd := filepath.Clean(filepath.Join(filepath.Dir(filepath.Dir(path)), ".."))
	attempts := map[string]conformanceAttempt{}
	_ = json.Unmarshal(state.Extensions["attempts"], &attempts)
	model.Evidence = readConformanceEvidence(
		filepath.Join(specd, "evidence.jsonl"), filepath.Dir(specd), change, state.Revision, attempts,
	)
	return model
}

func readConformanceDependencies(path string) map[string][]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	result := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 || strings.TrimSpace(cells[0]) == "id" || strings.HasPrefix(strings.TrimSpace(cells[0]), "---") {
			continue
		}
		id := strings.TrimSpace(cells[0])
		for _, dependency := range strings.Split(strings.TrimSpace(cells[3]), ";") {
			if dependency = strings.TrimSpace(dependency); dependency != "" {
				result[id] = append(result[id], dependency)
			}
		}
	}
	return result
}

func readConformanceEvidence(path, root, change string, revision uint64, attempts map[string]conformanceAttempt) map[string]string {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil
	}
	result := map[string]string{}
	head := ""
	if output, commandErr := exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD^{commit}").Output(); commandErr == nil {
		head = strings.TrimSpace(string(output))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var record struct {
			Change  string          `json:"change"`
			Payload json.RawMessage `json:"payload"`
		}
		var payload struct {
			Task          string `json:"task"`
			Attempt       string `json:"attempt"`
			HEAD          string `json:"head"`
			ApprovalHash  string `json:"approval_hash"`
			StateRevision uint64 `json:"state_revision"`
			Passed        bool   `json:"passed"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Change == change && json.Unmarshal(record.Payload, &payload) == nil {
			if payload.Passed {
				result[payload.Task] = "stale"
				attempt, exists := attempts[payload.Task]
				if exists && payload.Attempt == attempt.ID && payload.HEAD == head &&
					payload.HEAD == attempt.BaselineHEAD && payload.ApprovalHash == attempt.ApprovalHash &&
					payload.StateRevision == revision {
					result[payload.Task] = "applicable"
				}
			} else if _, exists := result[payload.Task]; !exists {
				result[payload.Task] = "none"
			}
		}
	}
	return result
}

func TestConformance(t *testing.T) {
	releaseStdin(t)
	collector := &traceCollector{}
	activeConformance = collector
	t.Cleanup(func() { activeConformance = nil })
	runReleaseJourneys(t)

	required := make([]string, 0, len(core.Operations()))
	for _, operation := range core.Operations() {
		if operation.Executable {
			required = append(required, operation.ID)
		}
	}
	journeys := make([]string, 14)
	for index := range journeys {
		journeys[index] = fmt.Sprintf("%02d", index+1)
	}
	if err := checkConformance(collector.steps, conformanceCoverage{
		Operations: required, Journeys: journeys, Refusal: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConformanceBites(t *testing.T) {
	base := conformanceStep{
		Schema: 1, Journey: "01", Operation: "status", Actor: "agent",
		Before: conformanceModel{Change: "sample", Exists: true, Lifecycle: "approved", Revision: 2},
		After:  conformanceModel{Change: "sample", Exists: true, Lifecycle: "approved", Revision: 2},
	}
	illegal := base
	illegal.Operation, illegal.Actor = "approve", "agent"
	illegal.Before.Lifecycle, illegal.Before.Revision = "planning", 1
	illegal.After.Lifecycle = "approved"
	mismatch := base
	mismatch.Operation, mismatch.Task = "complete", "task"
	mismatch.Before.Tasks = map[string]string{"task": "in_progress"}
	mismatch.Before.Evidence = map[string]string{"task": "stale"}
	mismatch.After.Tasks = map[string]string{"task": "completed"}
	mismatch.After.Revision = 3
	dependency := mismatch
	dependency.Before.Evidence = map[string]string{"task": "applicable"}
	dependency.Before.Dependencies = map[string][]string{"task": {"prior"}}
	dependency.Before.Tasks["prior"] = "pending"
	implBug := base
	implBug.ImplBug = "missing envelope"

	for name, testCase := range map[string]struct {
		steps    []conformanceStep
		coverage conformanceCoverage
		want     string
	}{
		"illegal transition": {[]conformanceStep{illegal}, conformanceCoverage{}, "IllegalTransition"},
		"state mismatch":     {[]conformanceStep{mismatch}, conformanceCoverage{}, "StateMismatch"},
		"coverage breach":    {[]conformanceStep{base}, conformanceCoverage{Operations: []string{"next"}}, "CoverageBreach"},
		"dependency breach":  {[]conformanceStep{dependency}, conformanceCoverage{}, "StateMismatch"},
		"implementation bug": {[]conformanceStep{implBug}, conformanceCoverage{}, "CoverageBreach"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkConformance(testCase.steps, testCase.coverage)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("got %v, want %s", err, testCase.want)
			}
		})
	}
}

func TestConformanceGeneratedSequences(t *testing.T) {
	random := rand.New(rand.NewSource(20260806))
	lifecycles := []string{"planning", "approved", "executing", "reconciling", "archived"}
	reads := []string{"check", "status", "next", "report"}
	for sequence := 0; sequence < 1000; sequence++ {
		lifecycle := lifecycles[random.Intn(len(lifecycles))]
		model := conformanceModel{Change: "generated", Exists: true, Lifecycle: lifecycle, Revision: uint64(random.Intn(20) + 1)}
		step := conformanceStep{
			Schema: 1, Journey: "generated", Operation: reads[random.Intn(len(reads))], Actor: "agent",
			Before: model, After: model,
		}
		if err := checkConformance([]conformanceStep{step}, conformanceCoverage{}); err != nil {
			t.Fatalf("legal sequence %d: %v", sequence, err)
		}

		step.Operation = "context"
		if lifecycle == "approved" {
			step.Before.Lifecycle, step.After.Lifecycle = "planning", "planning"
		}
		err := checkConformance([]conformanceStep{step}, conformanceCoverage{})
		if err == nil || !strings.Contains(err.Error(), "IllegalTransition") {
			t.Fatalf("illegal sequence %d: got %v", sequence, err)
		}
	}
}

func TestConformanceFixtures(t *testing.T) {
	base := conformanceStep{
		Schema: 1, Journey: "01", Operation: "status", Actor: "agent",
		Before: conformanceModel{Change: "sample", Exists: true, Lifecycle: "approved", Revision: 2},
		After:  conformanceModel{Change: "sample", Exists: true, Lifecycle: "approved", Revision: 2},
	}
	illegal := base
	illegal.Operation, illegal.Actor = "approve", "agent"
	illegal.Before.Lifecycle, illegal.Before.Revision = "planning", 1
	mismatch := base
	mismatch.Operation, mismatch.Task = "complete", "task"
	mismatch.Before.Tasks = map[string]string{"task": "in_progress"}
	mismatch.Before.Evidence = map[string]string{"task": "stale"}
	mismatch.After.Tasks = map[string]string{"task": "completed"}
	mismatch.After.Revision = 3

	for name, testCase := range map[string]struct {
		steps    []conformanceStep
		coverage conformanceCoverage
		want     string
	}{
		"good.jsonl":                   {[]conformanceStep{base}, conformanceCoverage{}, ""},
		"bad_illegal_transition.jsonl": {[]conformanceStep{illegal}, conformanceCoverage{}, "IllegalTransition"},
		"bad_state_mismatch.jsonl":     {[]conformanceStep{mismatch}, conformanceCoverage{}, "StateMismatch"},
		"bad_coverage_breach.jsonl":    {[]conformanceStep{base}, conformanceCoverage{Operations: []string{"next"}}, "CoverageBreach"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("conformance", "testdata", name)
			assertConformanceFixture(t, path, conformanceJSONL(t, testCase.steps))
			err := checkConformance(readConformanceFixture(t, path), testCase.coverage)
			if testCase.want == "" && err != nil {
				t.Fatal(err)
			}
			if testCase.want != "" && (err == nil || !strings.Contains(err.Error(), testCase.want)) {
				t.Fatalf("got %v, want %s", err, testCase.want)
			}
		})
	}
}

func readConformanceFixture(t *testing.T, path string) []conformanceStep {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var steps []conformanceStep
	for index, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		var step conformanceStep
		if err := json.Unmarshal(line, &step); err != nil {
			t.Fatalf("fixture %s line %d: %v", path, index+1, err)
		}
		steps = append(steps, step)
	}
	return steps
}

func conformanceJSONL(t *testing.T, steps []conformanceStep) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, step := range steps {
		raw, err := json.Marshal(step)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(raw)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func assertConformanceFixture(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("SPECD_WRITE_CONFORMANCE_FIXTURES") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing fixture %s (refresh with SPECD_WRITE_CONFORMANCE_FIXTURES=1): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture %s is stale; refresh with SPECD_WRITE_CONFORMANCE_FIXTURES=1", path)
	}
}

func checkConformance(steps []conformanceStep, coverage conformanceCoverage) error {
	seenOperations, seenJourneys := map[string]bool{}, map[string]bool{}
	seenRefusal := false
	for index, step := range steps {
		if step.Schema != 1 {
			return fmt.Errorf("StateMismatch step %d: schema %d", index, step.Schema)
		}
		seenOperations[step.Operation] = true
		seenJourneys[step.Journey] = true
		if step.ImplBug != "" {
			return fmt.Errorf("CoverageBreach step %d: %s", index, step.ImplBug)
		}
		if step.Refusal != "" {
			seenRefusal = true
			if strings.TrimSpace(step.NextAction) == "" {
				return fmt.Errorf("StateMismatch step %d: refusal %s has no next action", index, step.Refusal)
			}
			if !sameConformanceModel(step.Before, step.After) {
				return fmt.Errorf("StateMismatch step %d: refusal %s changed state", index, step.Refusal)
			}
			continue
		}
		if step.Exit != 0 {
			if step.Before.Revision != step.After.Revision {
				return fmt.Errorf("StateMismatch step %d: failed operation moved revision", index)
			}
			continue
		}
		if err := checkConformanceTransition(step); err != nil {
			return fmt.Errorf("step %d %s: %w", index, step.Operation, err)
		}
	}
	for _, operation := range coverage.Operations {
		if _, exists := conformanceRules[operation]; !exists {
			return fmt.Errorf("CoverageBreach: operation %s has no independent rule", operation)
		}
		if !seenOperations[operation] {
			return fmt.Errorf("CoverageBreach: operation %s emitted no step", operation)
		}
	}
	for _, journey := range coverage.Journeys {
		if !seenJourneys[journey] {
			return fmt.Errorf("CoverageBreach: journey %s emitted no step", journey)
		}
	}
	if coverage.Refusal && !seenRefusal {
		return errors.New("CoverageBreach: no refusal step was exercised")
	}
	return nil
}

func checkConformanceTransition(step conformanceStep) error {
	rule, exists := conformanceRules[step.Operation]
	if !exists {
		return fmt.Errorf("CoverageBreach: operation %s has no independent rule", step.Operation)
	}
	if !csvContains(rule.actors, step.Actor) {
		return errors.New("IllegalTransition: operation used an illegal actor route")
	}
	if rule.lifecycles != "" && (!step.Before.Exists || !csvContains(rule.lifecycles, step.Before.Lifecycle)) {
		return fmt.Errorf("IllegalTransition: %s is not legal from %s", step.Operation, step.Before.Lifecycle)
	}
	if step.After.Revision < step.Before.Revision {
		return errors.New("StateMismatch: revision decreased")
	}
	switch rule.kind {
	case "unchanged":
		if !sameConformanceModel(step.Before, step.After) {
			return errors.New("StateMismatch: observation operation changed modeled state")
		}
	case "new":
		if step.Before.Exists || step.After.Lifecycle != "planning" {
			return errors.New("IllegalTransition: new did not create planning state")
		}
		if step.After.Revision != 1 {
			return errors.New("StateMismatch: new did not create revision 1")
		}
	case "approve":
		if step.Before.Lifecycle != "planning" || step.After.Lifecycle != "approved" {
			return errors.New("IllegalTransition: approval lifecycle")
		}
		if step.After.Revision != step.Before.Revision+1 {
			return errors.New("StateMismatch: approval did not advance one revision")
		}
	case "reopen":
		if step.Before.Lifecycle != "approved" || step.After.Lifecycle != "planning" {
			return errors.New("IllegalTransition: reopen lifecycle")
		}
		if step.After.Revision != step.Before.Revision+1 || len(step.After.Tasks) != 0 {
			return errors.New("StateMismatch: reopen did not reset task activity at one new revision")
		}
		for _, applicability := range step.After.Evidence {
			if applicability == "applicable" {
				return errors.New("StateMismatch: reopen retained applicable evidence")
			}
		}
	case "start":
		if !slices.Contains([]string{"", "pending", "failed"}, step.Before.Tasks[step.Task]) ||
			step.After.Tasks[step.Task] != "in_progress" {
			return errors.New("IllegalTransition: start activity")
		}
		if step.After.Revision != step.Before.Revision+1 {
			return errors.New("StateMismatch: start did not advance one revision")
		}
	case "verify":
		if step.After.Revision != step.Before.Revision || step.Before.Tasks[step.Task] != "in_progress" ||
			step.After.Tasks[step.Task] != "in_progress" || step.After.Evidence[step.Task] != "applicable" {
			return errors.New("StateMismatch: verification changed state or ran without an active attempt")
		}
	case "complete":
		if step.Before.Tasks[step.Task] != "in_progress" || step.Before.Evidence[step.Task] != "applicable" ||
			step.After.Tasks[step.Task] != "completed" {
			return errors.New("StateMismatch: completion lacks passing evidence or activity")
		}
		for _, dependency := range step.Before.Dependencies[step.Task] {
			if step.Before.Tasks[dependency] != "completed" {
				return errors.New("StateMismatch: completion preceded a dependency")
			}
		}
		if step.After.Revision != step.Before.Revision+1 {
			return errors.New("StateMismatch: completion did not advance one revision")
		}
	case "sync":
		if step.After.Lifecycle != "reconciling" {
			return errors.New("IllegalTransition: sync lifecycle")
		}
		wantRevision := step.Before.Revision
		if step.Before.Lifecycle == "approved" {
			wantRevision++
		}
		if step.After.Revision != wantRevision {
			return errors.New("StateMismatch: sync revision")
		}
	case "archive":
		if step.Before.Lifecycle != "reconciling" || step.After.Lifecycle != "archived" {
			return errors.New("IllegalTransition: archive lifecycle")
		}
		if step.After.Revision != step.Before.Revision+1 {
			return errors.New("StateMismatch: archive did not advance one revision")
		}
	}
	return nil
}

func csvContains(values, value string) bool {
	return slices.Contains(strings.Split(values, ","), value)
}

func sameConformanceModel(a, b conformanceModel) bool {
	aRaw, _ := json.Marshal(a)
	bRaw, _ := json.Marshal(b)
	return string(aRaw) == string(bRaw)
}
