package context

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestManifestDeterministicTypedLanesAndAuthority(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	approval := manifestApproval(7)
	input := manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision: 7, Approval: approval, Frontier: []string{"T1"},
		TaskReadiness:  []core.TaskReadiness{{ID: "T1", Activity: core.TaskPending, Readiness: core.ReadinessReady}},
		RequiredInputs: []SourceRef{{Path: "docs/input.md"}},
	}
	first, err := buildManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildManifest(input)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("manifest instability:\n%#v\n%#v\n%v", first, second, err)
	}
	if first.Version != ManifestVersion || first.ManifestHash == "" ||
		first.FrontierHash == "" || first.ApprovalHash != approval.Approval.AggregateHash {
		t.Fatalf("identities = %#v", first)
	}
	if first.Authority.Assurance != "advisory" ||
		!reflect.DeepEqual(first.Authority.AllowedWritePaths, []string{"internal/existing.go", "internal/new.go"}) ||
		!reflect.DeepEqual(first.Authority.DeniedWritePaths, []string{".specd/**"}) ||
		first.Authority.Verify != "go test ./..." {
		t.Fatalf("authority = %#v", first.Authority)
	}
	lanes := map[string]Lane{}
	for _, item := range first.Items {
		lanes[item.Path] = item.Lane
	}
	if lanes["docs/input.md"] != LaneRequiredInput ||
		lanes["internal/existing.go"] != LaneOptionalExistingOutput ||
		lanes["internal/new.go"] != LaneProspectiveOutput {
		t.Fatalf("lanes = %#v", lanes)
	}
	for _, kind := range []string{"delta_requirement", "accepted_spec", "design", "task"} {
		found := false
		for _, item := range first.Items {
			found = found || item.Kind == kind
		}
		if !found {
			t.Fatalf("manifest lacks %s: %#v", kind, first.Items)
		}
	}
}

func TestManifestRequiredBudgetNeverTruncates(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	input := manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision:  7,
		Approval:       manifestApproval(7),
		Frontier:       []string{"T1"},
		TaskReadiness:  []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
		RequiredInputs: []SourceRef{{Path: "docs/input.md"}},
		BudgetBytes:    1,
	}
	manifest, err := buildManifest(input)
	if err == nil || manifest.Version != "" || !strings.Contains(err.Error(), "required context is") {
		t.Fatalf("manifest = %#v, error = %v", manifest, err)
	}
}

func TestManifestIncludesExactDependencySummary(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	changePlan.Tasks.Tasks[0].DependsOn = []string{"D0"}
	changePlan.Tasks.Tasks = append(changePlan.Tasks.Tasks, plan.Task{ID: "D0", Valid: true})
	input := manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision: 7,
		Approval:      manifestApproval(7),
		Frontier:      []string{"T1"},
		TaskReadiness: []core.TaskReadiness{
			{ID: "T1", Activity: core.TaskPending, Readiness: core.ReadinessReady},
			{ID: "D0", Activity: core.TaskCompleted, Readiness: core.ReadinessTerminal},
		},
	}
	manifest, err := buildManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest.Items {
		if item.Kind == "dependency_summary" && item.Path == "task:D0" &&
			strings.Contains(item.Content, `"activity":"completed"`) && item.Digest != "" {
			return
		}
	}
	t.Fatalf("dependency summary missing: %#v", manifest.Items)
}

func TestManifestOptionalOmissionRetainsDigest(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	base := manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision: 7,
		Approval:      manifestApproval(7),
		Frontier:      []string{"T1"}, TaskReadiness: []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
	}
	unlimited, err := buildManifest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.BudgetBytes = unlimited.RequiredBytes
	limited, err := buildManifest(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Omissions) != 1 || limited.Omissions[0].Path != "internal/existing.go" ||
		limited.Omissions[0].Digest == "" ||
		limited.Omissions[0].Reason != "optional output omitted to satisfy context budget" {
		t.Fatalf("omissions = %#v", limited.Omissions)
	}
	for _, item := range limited.Items {
		if item.Path == "internal/new.go" && item.Lane == LaneProspectiveOutput {
			return
		}
	}
	t.Fatal("budget omitted prospective write authority")
}

func TestManifestRefusesFrontierAndApprovalMismatch(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	base := manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision: 7, Frontier: []string{"T1"},
		Approval:      manifestApproval(7),
		TaskReadiness: []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
	}
	stale := base
	stale.Approval.Current = false
	if manifest, err := buildManifest(stale); err == nil || manifest.Version != "" {
		t.Fatalf("stale approval returned %#v, %v", manifest, err)
	}
	blocked := base
	blocked.Frontier = nil
	if manifest, err := buildManifest(blocked); err == nil || manifest.Version != "" {
		t.Fatalf("non-frontier task returned %#v, %v", manifest, err)
	}
	wrongRevision := base
	wrongRevision.Approval = manifestApproval(8)
	if manifest, err := buildManifest(wrongRevision); err == nil || manifest.Version != "" {
		t.Fatalf("wrong-revision approval returned %#v, %v", manifest, err)
	}
	invented := base
	invented.Frontier = []string{"T1", "invented"}
	if manifest, err := buildManifest(invented); err == nil || manifest.Version != "" {
		t.Fatalf("invented frontier returned %#v, %v", manifest, err)
	}
}

func TestManifestAcceptsCurrentApprovalAcrossNonSemanticRevision(t *testing.T) {
	root, changePlan, _ := manifestFixture(t)
	input := manifestInput{
		Root: root, Change: "safe-change", TaskID: "T1", Plan: changePlan,
		StateRevision: 8, Approval: manifestApproval(7), Frontier: []string{"T1"},
		TaskReadiness: []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
	}
	if manifest, err := buildManifest(input); err != nil || manifest.StateRevision != 8 {
		t.Fatalf("non-semantic revision manifest = %#v, %v", manifest, err)
	}
	if manifest, err := BuildManifest(core.ReadinessSnapshot{}, "T1", nil, 0); err == nil || manifest.Version != "" {
		t.Fatalf("unsealed snapshot accepted: %#v, %v", manifest, err)
	}
}

func TestManifestDerivesTaskAndRejectsManagedWriteScope(t *testing.T) {
	root, changePlan, _ := manifestFixture(t)
	changePlan.Tasks.Tasks[0].Files = []string{".specd/history.jsonl"}
	input := manifestInput{
		Root: root, Change: "safe-change", TaskID: "T1", Plan: changePlan,
		StateRevision: 7, Approval: manifestApproval(7), Frontier: []string{"T1"},
		TaskReadiness: []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
	}
	manifest, err := buildManifest(input)
	if err == nil || manifest.Version != "" || !strings.Contains(err.Error(), "managed state") {
		t.Fatalf("managed write scope accepted: %#v, %v", manifest, err)
	}
	input.TaskID = "invented"
	if manifest, err := buildManifest(input); err == nil || manifest.Version != "" {
		t.Fatalf("task outside canonical plan accepted: %#v, %v", manifest, err)
	}
}

func manifestFixture(t *testing.T) (string, plan.Change, plan.Task) {
	t.Helper()
	root := t.TempDir()
	for relative, content := range map[string]string{
		"docs/input.md":                                   "input\n",
		"internal/existing.go":                            "package existing\n",
		".specd/specs/sample/spec.md":                     "accepted truth\n",
		".specd/changes/safe-change/design.md":            "design bytes\n",
		".specd/changes/safe-change/tasks.md":             "task bytes\n",
		".specd/changes/safe-change/specs/sample/spec.md": "delta bytes\n",
	} {
		writeResolveFile(t, root, relative, content)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	designPath, _ := owner.ChangeDesign("safe-change")
	tasksPath, _ := owner.ChangeTasks("safe-change")
	deltaPath, _ := owner.ChangeSpec("safe-change", "sample")
	task := plan.Task{
		ID: "T1", Role: "builder",
		Files: []string{"internal/existing.go", "internal/new.go"},
		References: []plan.RequirementReference{{
			Capability: "sample", Requirement: "Stable", Location: plan.Location{Path: tasksPath, Line: 3},
		}},
		Verify: "go test ./...", Source: []byte("| T1 | row |\n"),
		Location: plan.Location{Path: tasksPath, Line: 3, Column: 1}, Valid: true,
	}
	changePlan := plan.Change{
		Root: root, Name: "safe-change",
		Design: plan.Design{Source: plan.Source{Path: designPath, Bytes: []byte("design bytes\n"), Present: true}},
		Tasks:  plan.Tasks{Source: plan.Source{Path: tasksPath, Bytes: []byte("task bytes\n"), Present: true}, Tasks: []plan.Task{task}},
		Deltas: []plan.CapabilityDelta{{
			Capability: "sample", Source: plan.Source{Path: deltaPath, Bytes: []byte("delta bytes\n"), Present: true},
			Operations: []plan.Operation{{Kind: plan.Added, Requirement: &plan.Requirement{
				Name: "Stable", Identity: plan.NormalizeRequirementIdentity("Stable"),
				Raw:      []byte("### Requirement: Stable\nThe system MUST work.\n"),
				Location: plan.Location{Path: deltaPath, Line: 3, Column: 1},
			}}},
		}},
	}
	return root, changePlan, task
}

func TestManifestAssemblyIsReadOnly(t *testing.T) {
	root, changePlan, task := manifestFixture(t)
	statePath := filepath.Join(root, ".specd", "changes", "safe-change", "state.json")
	if err := os.WriteFile(statePath, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(statePath)
	_, err := buildManifest(manifestInput{
		Root: root, Change: "safe-change", TaskID: task.ID, Plan: changePlan,
		StateRevision: 1, Frontier: []string{"T1"},
		Approval:      manifestApproval(1),
		TaskReadiness: []core.TaskReadiness{{ID: "T1", Readiness: core.ReadinessReady}},
	})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(statePath)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("manifest assembly mutated state")
	}
}

func manifestApproval(revision uint64) core.ApprovalStatus {
	return core.ApprovalStatus{
		Change: "safe-change", Current: true,
		Approval: &core.ApprovalRecord{
			Change: "safe-change", RevisionAfter: revision,
			AggregateHash: strings.Repeat("a", 64),
		},
	}
}
