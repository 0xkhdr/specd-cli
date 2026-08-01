package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestReadinessParallelFrontierAndActiveSibling(t *testing.T) {
	tasks := readinessTasks(task("Z"), task("A"), task("M"))
	approval := ApprovalStatus{Current: true, Approval: &ApprovalRecord{}}
	model := ProjectReadiness(tasks, nil, "approved", approval)
	if got, want := model.Frontier(), []string{"Z", "A", "M"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frontier = %q, want %q", got, want)
	}
	active := map[string]json.RawMessage{"A": json.RawMessage(`"in_progress"`)}
	model = ProjectReadiness(tasks, active, "approved", approval)
	if got, want := model.Frontier(), []string{"Z", "M"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active frontier = %q, want %q", got, want)
	}
	row, _ := model.Task("A")
	if row.Readiness != ReadinessActive || row.Blocker.Code != "task_active" {
		t.Fatalf("active row = %#v", row)
	}
}

func TestReadinessChainDiamondAndDependencyStates(t *testing.T) {
	tasks := readinessTasks(
		task("root"), task("left", "root"), task("right", "root"),
		task("leaf", "left", "right"),
	)
	approval := ApprovalStatus{Current: true, Approval: &ApprovalRecord{}}
	model := ProjectReadiness(tasks, nil, "approved", approval)
	if got, want := model.Frontier(), []string{"root"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frontier = %q, want %q", got, want)
	}
	leaf, _ := model.Task("leaf")
	if leaf.Readiness != ReadinessWaitingDependency ||
		!reflect.DeepEqual(leaf.Dependencies, []string{"left", "right"}) ||
		leaf.Blocker.Code != "dependency_incomplete" {
		t.Fatalf("leaf = %#v", leaf)
	}
	completed := map[string]json.RawMessage{
		"root": json.RawMessage(`"completed"`),
	}
	model = ProjectReadiness(tasks, completed, "approved", approval)
	if got, want := model.Frontier(), []string{"left", "right"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("diamond frontier = %q, want %q", got, want)
	}
}

func TestReadinessBlockerPriority(t *testing.T) {
	tasks := readinessTasks(task("parent"), task("child", "parent"))
	stale := ApprovalStatus{Current: false, Approval: &ApprovalRecord{}}
	model := ProjectReadiness(tasks, map[string]json.RawMessage{
		"child": json.RawMessage(`"blocked"`),
	}, "approved", stale)
	child, _ := model.Task("child")
	if child.Readiness != ReadinessWaitingApproval || child.Blocker.Code != "approval_stale" ||
		child.Blocker.Owner != "human" || len(child.Dependencies) != 1 {
		t.Fatalf("priority row = %#v", child)
	}
	missing := ProjectReadiness(tasks, nil, "planning", ApprovalStatus{})
	child, _ = missing.Task("child")
	if child.Blocker.Code != "change_not_approved" {
		t.Fatalf("missing approval row = %#v", child)
	}
}

func TestReadinessBlockedFailedTerminalAndComplete(t *testing.T) {
	tasks := readinessTasks(task("failed"), task("blocked"), task("done"))
	persisted := map[string]json.RawMessage{
		"failed":  json.RawMessage(`"failed"`),
		"blocked": json.RawMessage(`"blocked"`),
		"done":    json.RawMessage(`"completed"`),
	}
	model := ProjectReadiness(tasks, persisted, "approved", ApprovalStatus{Current: true})
	for _, id := range []string{"failed", "blocked"} {
		row, _ := model.Task(id)
		if row.Readiness != ReadinessBlocked || row.Blocker.Code != "task_blocked" {
			t.Fatalf("%s = %#v", id, row)
		}
	}
	done, _ := model.Task("done")
	if done.Readiness != ReadinessTerminal || done.Blocker.Code != "task_completed" || model.AllComplete() {
		t.Fatalf("terminal projection = %#v complete=%v", done, model.AllComplete())
	}
	all := ProjectReadiness(readinessTasks(task("done")), map[string]json.RawMessage{
		"done": json.RawMessage(`"completed"`),
	}, "approved", ApprovalStatus{Current: true})
	if !all.AllComplete() || len(all.Frontier()) != 0 {
		t.Fatalf("all complete = %#v", all)
	}
	empty := ProjectReadiness(readinessTasks(), nil, "approved", ApprovalStatus{Current: true})
	if !empty.AllComplete() || len(empty.Tasks()) != 0 || len(empty.Frontier()) != 0 {
		t.Fatalf("empty = %#v", empty)
	}
}

func TestReadinessInvalidFailsClosed(t *testing.T) {
	cases := []struct {
		name      string
		tasks     plan.Tasks
		persisted map[string]json.RawMessage
		lifecycle string
	}{
		{"cycle", readinessTasks(task("A", "B"), task("B", "A")), nil, "approved"},
		{"unknown activity", readinessTasks(task("A")), map[string]json.RawMessage{"B": json.RawMessage(`"pending"`)}, "approved"},
		{"unknown lifecycle", readinessTasks(task("A")), nil, "future"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := ProjectReadiness(test.tasks, test.persisted, test.lifecycle, ApprovalStatus{Current: true})
			blocker := model.PlanBlocker()
			if blocker == nil || blocker.Code != "plan_invalid" || len(model.Frontier()) != 0 {
				t.Fatalf("model = %#v blocker=%#v", model, blocker)
			}
		})
	}
}

func TestReadinessCanRetryFailedTask(t *testing.T) {
	tasks := readinessTasks(task("A"), task("B", "A"))
	fresh := ApprovalStatus{Current: true}
	failedB := map[string]json.RawMessage{
		"A": json.RawMessage(`"completed"`), "B": json.RawMessage(`"failed"`),
	}
	model := ProjectReadiness(tasks, failedB, "approved", fresh)
	if !model.CanRetry("B") {
		t.Fatalf("failed task with completed dependency cannot retry: %#v", model.Tasks())
	}
	if model.CanRetry("A") || model.InFrontier("B") {
		t.Fatalf("retry leaked into frontier or completed task: %#v", model.Tasks())
	}
	pendingA := map[string]json.RawMessage{"B": json.RawMessage(`"failed"`)}
	if ProjectReadiness(tasks, pendingA, "approved", fresh).CanRetry("B") {
		t.Fatal("failed task retried with an incomplete dependency")
	}
	stale := ApprovalStatus{Current: false}
	if ProjectReadiness(tasks, failedB, "approved", stale).CanRetry("B") {
		t.Fatal("failed task retried under stale approval")
	}
	if ProjectReadiness(tasks, failedB, "planning", fresh).CanRetry("B") {
		t.Fatal("failed task retried before approval")
	}
}

func TestReadinessLegalUnprojectedLifecycleBlamesHarness(t *testing.T) {
	for _, lifecycle := range []string{"executing", "reconciling", "archived"} {
		model := ProjectReadiness(readinessTasks(task("A")), nil, lifecycle, ApprovalStatus{Current: true})
		blocker := model.PrimaryBlocker()
		if blocker == nil || blocker.Code != "lifecycle_unsupported" ||
			blocker.Owner != "harness" || len(model.Frontier()) != 0 {
			t.Fatalf("%s blocker = %#v", lifecycle, blocker)
		}
		if model.PlanBlocker() != nil {
			t.Fatalf("%s reported an authoring plan defect: %#v", lifecycle, model.PlanBlocker())
		}
	}
}

func TestReadinessPureAndImmutableQueries(t *testing.T) {
	persisted := map[string]json.RawMessage{"A": json.RawMessage(`"pending"`)}
	model := ProjectReadiness(readinessTasks(task("A")), persisted, "approved", ApprovalStatus{Current: true})
	before := append([]byte(nil), persisted["A"]...)
	model.Frontier()[0] = "changed"
	rows := model.Tasks()
	rows[0].Dependencies = append(rows[0].Dependencies, "changed")
	if !model.InFrontier("A") || !reflect.DeepEqual(before, []byte(persisted["A"])) {
		t.Fatal("projection mutated input or query result mutated model")
	}
}

func TestReadinessSnapshotIsSealedAndCopiesPlan(t *testing.T) {
	root := approvedStatusRoot(t)
	snapshot, err := LoadReadinessSnapshot(root, "safe-change")
	if err != nil {
		t.Fatal(err)
	}
	authored := snapshot.Plan()
	authored.Tasks.Tasks[0].ID = "forged"
	authored.Tasks.Source.Bytes[0] = 'X'
	if !snapshot.Valid() || snapshot.Plan().Tasks.Tasks[0].ID != "T1" ||
		snapshot.Model().InFrontier("forged") {
		t.Fatal("caller mutated sealed readiness snapshot")
	}
}

func readinessTasks(rows ...plan.Task) plan.Tasks {
	return plan.Tasks{Tasks: rows}
}
