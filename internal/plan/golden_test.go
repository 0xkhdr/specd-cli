package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

type goldenChange struct {
	Name        string        `json:"name"`
	Proposal    []string      `json:"proposal_sections"`
	Design      []string      `json:"design_sections"`
	Deltas      []goldenDelta `json:"deltas"`
	Tasks       []goldenTask  `json:"tasks"`
	Waves       []TaskWave    `json:"waves"`
	Trace       []goldenTrace `json:"trace"`
	Diagnostics []string      `json:"diagnostics"`
}

type goldenDelta struct {
	Capability string   `json:"capability"`
	Operations []string `json:"operations"`
}

type goldenTask struct {
	ID        string   `json:"id"`
	Files     []string `json:"files"`
	DependsOn []string `json:"depends_on"`
}

type goldenTrace struct {
	Capability  string   `json:"capability"`
	Requirement string   `json:"requirement"`
	Tasks       []string `json:"tasks"`
}

func TestPlanGoldenByteContract(t *testing.T) {
	root := materializeChange(t, "minimal-change")
	accepted := filepath.Join(root, ".specd", "specs", "accounts")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accepted, "spec.md"), []byte("# Accounts\n\n## Requirements\n### Requirement: Stable locking\nThe system MUST lock.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	change := ParseChange(owner, "add-locking")
	projected := goldenChange{Name: change.Name, Waves: change.Tasks.Waves}
	for _, section := range change.Proposal.Sections {
		projected.Proposal = append(projected.Proposal, section.Name)
	}
	for _, section := range change.Design.Sections {
		projected.Design = append(projected.Design, section.Name)
	}
	for _, delta := range change.Deltas {
		item := goldenDelta{Capability: delta.Capability}
		for _, operation := range delta.Operations {
			name := operation.From + operation.To
			if operation.Requirement != nil {
				name = operation.Requirement.Name
			}
			item.Operations = append(item.Operations, string(operation.Kind)+": "+name)
		}
		projected.Deltas = append(projected.Deltas, item)
	}
	for _, task := range change.Tasks.Tasks {
		projected.Tasks = append(projected.Tasks, goldenTask{ID: task.ID, Files: task.Files, DependsOn: task.DependsOn})
	}
	for _, trace := range change.Trace.Requirements {
		projected.Trace = append(projected.Trace, goldenTrace{
			Capability: trace.Capability, Requirement: trace.Requirement, Tasks: trace.Tasks,
		})
	}
	for _, diagnostic := range change.Diagnostics {
		projected.Diagnostics = append(projected.Diagnostics, diagnostic.Code)
	}
	got, err := json.MarshalIndent(projected, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertPlanGolden(t, filepath.Join("testdata", "good_minimal_change.expected.json"), append(got, '\n'))
}

// assertPlanGolden makes schema, ordering, and whitespace changes visible in
// review. Refresh can bless a bad change; it exposes bytes, not correctness.
func assertPlanGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("SPECD_WRITE_PLAN_FIXTURES") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing fixture %s (refresh with SPECD_WRITE_PLAN_FIXTURES=1 go test ./internal/plan -run TestPlanGoldenByteContract): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture %s is stale; refresh with SPECD_WRITE_PLAN_FIXTURES=1 go test ./internal/plan -run TestPlanGoldenByteContract", path)
	}
}
