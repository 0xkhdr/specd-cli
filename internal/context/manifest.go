package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const ManifestVersion = "context-manifest/v1"

type ManifestItem struct {
	Kind     string        `json:"kind"`
	Path     string        `json:"path"`
	Lane     Lane          `json:"lane"`
	Selector string        `json:"selector,omitempty"`
	Location plan.Location `json:"location"`
	Digest   string        `json:"digest,omitempty"`
	Content  string        `json:"content,omitempty"`
}

type Omission struct {
	Path   string `json:"path"`
	Digest string `json:"digest,omitempty"`
	Reason string `json:"reason"`
}

type Authority struct {
	AllowedWritePaths []string `json:"allowedWritePaths"`
	DeniedWritePaths  []string `json:"deniedWritePaths"`
	OperationClass    string   `json:"operationClass"`
	Verify            string   `json:"verify"`
	Assurance         string   `json:"assurance"`
}

type Manifest struct {
	Version       string         `json:"version"`
	Root          string         `json:"root"`
	Change        string         `json:"change"`
	Task          string         `json:"task"`
	Role          string         `json:"role"`
	StateRevision uint64         `json:"stateRevision"`
	ApprovalHash  string         `json:"approvalHash"`
	Frontier      []string       `json:"frontier"`
	FrontierHash  string         `json:"frontierHash"`
	Items         []ManifestItem `json:"items"`
	Omissions     []Omission     `json:"omissions"`
	RequiredBytes int            `json:"requiredBytes"`
	OptionalBytes int            `json:"optionalBytes"`
	Authority     Authority      `json:"authority"`
	ManifestHash  string         `json:"manifestHash"`
}

type manifestInput struct {
	Root           string
	Change         string
	TaskID         string
	Plan           plan.Change
	StateRevision  uint64
	Approval       core.ApprovalStatus
	Frontier       []string
	TaskReadiness  []core.TaskReadiness
	RequiredInputs []SourceRef
	BudgetBytes    int
}

type ManifestError struct {
	Code   string
	Owner  string
	Reason string
	Next   string
}

func (e *ManifestError) Error() string {
	return fmt.Sprintf("%s: %s; owner: %s; next: %s", e.Code, e.Reason, e.Owner, e.Next)
}

func BuildManifest(snapshot core.ReadinessSnapshot, taskID string, requiredInputs []SourceRef, budgetBytes int) (Manifest, error) {
	if !snapshot.Valid() {
		return Manifest{}, manifestFailure("context_input_invalid", "readiness snapshot is invalid", "regenerate context")
	}
	model := snapshot.Model()
	return buildManifest(manifestInput{
		Root: snapshot.Root(), Change: snapshot.Change(), TaskID: taskID,
		Plan: snapshot.Plan(), StateRevision: snapshot.StateRevision(),
		Approval: snapshot.Approval(), Frontier: model.Frontier(),
		TaskReadiness: model.Tasks(), RequiredInputs: requiredInputs, BudgetBytes: budgetBytes,
	})
}

func buildManifest(input manifestInput) (Manifest, error) {
	if input.StateRevision == 0 || input.Change == "" || input.TaskID == "" {
		return Manifest{}, manifestFailure("context_input_invalid", "manifest identities are incomplete", "regenerate context")
	}
	canonicalRoot, err := canonicalRoot(input.Root)
	if err != nil {
		return Manifest{}, manifestFailure("context_root_invalid", err.Error(), "resolve the named path")
	}
	if input.Plan.Name != input.Change || filepath.Clean(input.Plan.Root) != canonicalRoot {
		return Manifest{}, manifestFailure("context_snapshot_mismatch",
			"canonical plan does not match selected root and change", "regenerate context")
	}
	task, err := selectedPlanTask(input.Plan.Tasks, input.TaskID)
	if err != nil {
		return Manifest{}, err
	}
	if !input.Approval.Current || input.Approval.Approval == nil {
		return Manifest{}, manifestFailure("context_approval_stale", "change approval is not current", "revise plan then reapprove")
	}
	approval := input.Approval.Approval
	if input.Approval.Change != input.Change || approval.Change != input.Change ||
		approval.RevisionAfter > input.StateRevision || !validDigest(approval.AggregateHash) {
		return Manifest{}, manifestFailure("context_snapshot_mismatch",
			"approval does not bind the selected change and revision", "regenerate context")
	}
	derivedFrontier, err := validateReadinessFacts(input.Plan.Tasks, input.TaskReadiness)
	if err != nil {
		return Manifest{}, err
	}
	if !slices.Equal(input.Frontier, derivedFrontier) {
		return Manifest{}, manifestFailure("context_frontier_mismatch",
			"frontier does not match canonical readiness facts", "regenerate context")
	}
	if !slices.Contains(derivedFrontier, task.ID) {
		return Manifest{}, manifestFailure("context_frontier_mismatch",
			fmt.Sprintf("task %q is not in the current frontier", task.ID), "regenerate context")
	}
	for _, path := range task.Files {
		if path == ".specd" || strings.HasPrefix(path, ".specd/") {
			return Manifest{}, manifestFailure("context_scope_invalid",
				fmt.Sprintf("declared write path %q intersects managed state", path), "revise plan then reapprove")
		}
	}

	items, err := semanticItems(canonicalRoot, input, task)
	if err != nil {
		return Manifest{}, err
	}
	resolved, err := ResolveContextLanes(canonicalRoot, input.RequiredInputs, outputRefs(task))
	if err != nil {
		return Manifest{}, err
	}
	for _, source := range resolved {
		items = append(items, manifestItem("source", source))
	}
	for index := range items {
		if items[index].Location.Path == "" {
			continue
		}
		locationPath, err := projectRelative(canonicalRoot, items[index].Location.Path)
		if err != nil {
			return Manifest{}, manifestFailure("context_path_unsafe", err.Error(), "resolve the named path")
		}
		items[index].Location.Path = locationPath
	}

	requiredBytes := 0
	for _, item := range items {
		if item.Lane == LaneRequiredInput {
			requiredBytes += len(item.Content)
		}
	}
	if input.BudgetBytes > 0 && requiredBytes > input.BudgetBytes {
		return Manifest{}, manifestFailure("context_budget_exceeded",
			fmt.Sprintf("required context is %d bytes; budget is %d bytes", requiredBytes, input.BudgetBytes),
			"revise plan then reapprove")
	}

	kept := make([]ManifestItem, 0, len(items))
	omissions := []Omission{}
	optionalBytes := 0
	for _, item := range items {
		if item.Lane != LaneOptionalExistingOutput {
			kept = append(kept, item)
			continue
		}
		cost := len(item.Content)
		if input.BudgetBytes > 0 && requiredBytes+optionalBytes+cost > input.BudgetBytes {
			omissions = append(omissions, Omission{
				Path: item.Path, Digest: item.Digest, Reason: "optional output omitted to satisfy context budget",
			})
			continue
		}
		optionalBytes += cost
		kept = append(kept, item)
	}

	frontier := append([]string(nil), input.Frontier...)
	manifest := Manifest{
		Version: ManifestVersion, Root: canonicalRoot, Change: input.Change,
		Task: task.ID, Role: task.Role, StateRevision: input.StateRevision,
		ApprovalHash: input.Approval.Approval.AggregateHash,
		Frontier:     frontier, FrontierHash: digestJSON(frontier),
		Items: kept, Omissions: omissions,
		RequiredBytes: requiredBytes, OptionalBytes: optionalBytes,
		Authority: Authority{
			AllowedWritePaths: append([]string(nil), task.Files...),
			DeniedWritePaths:  []string{".specd/**"},
			OperationClass:    "task_edit_and_verify",
			Verify:            task.Verify,
			Assurance:         "advisory",
		},
	}
	manifest.ManifestHash = digestJSON(manifest)
	return manifest, nil
}

func semanticItems(root string, input manifestInput, task plan.Task) ([]ManifestItem, error) {
	var items []ManifestItem
	acceptedSeen := map[string]bool{}
	for _, reference := range task.References {
		found := false
		for _, delta := range input.Plan.Deltas {
			if delta.Capability != reference.Capability {
				continue
			}
			for _, operation := range delta.Operations {
				requirement := operation.Requirement
				if requirement == nil ||
					requirement.Identity != plan.NormalizeRequirementIdentity(reference.Requirement) {
					continue
				}
				path, err := projectRelative(root, requirement.Location.Path)
				if err != nil {
					return nil, manifestFailure("context_path_unsafe", err.Error(), "resolve the named path")
				}
				items = append(items, inlineItem(
					"delta_requirement", path, requirement.Location, requirement.Raw,
				))
				found = true
				break
			}
		}
		if !found {
			return nil, manifestFailure("context_requirement_missing",
				fmt.Sprintf("task reference %s/Requirement: %s is unresolved",
					reference.Capability, reference.Requirement),
				"revise plan then reapprove")
		}
		if acceptedSeen[reference.Capability] {
			continue
		}
		acceptedSeen[reference.Capability] = true
		acceptedPath := filepath.ToSlash(filepath.Join(".specd", "specs", reference.Capability, "spec.md"))
		accepted, err := ResolveContextLanes(root, nil, []SourceRef{{Path: acceptedPath}})
		if err != nil {
			return nil, err
		}
		if len(accepted) == 1 && accepted[0].Lane == LaneOptionalExistingOutput {
			accepted[0].Lane = LaneRequiredInput
			items = append(items, manifestItem("accepted_spec", accepted[0]))
		}
	}
	designPath, err := projectRelative(root, input.Plan.Design.Source.Path)
	if err != nil {
		return nil, manifestFailure("context_path_unsafe", err.Error(), "resolve the named path")
	}
	items = append(items, inlineItem("design", designPath,
		plan.Location{Path: input.Plan.Design.Source.Path, Line: 1, Column: 1},
		input.Plan.Design.Source.Bytes))
	taskPath, err := projectRelative(root, input.Plan.Tasks.Source.Path)
	if err != nil {
		return nil, manifestFailure("context_path_unsafe", err.Error(), "resolve the named path")
	}
	items = append(items, inlineItem("task", taskPath, task.Location, task.Source))

	readiness := make(map[string]core.TaskReadiness, len(input.TaskReadiness))
	for _, row := range input.TaskReadiness {
		readiness[row.ID] = row
	}
	for _, dependency := range task.DependsOn {
		row, ok := readiness[dependency]
		if !ok {
			return nil, manifestFailure("context_dependency_missing",
				fmt.Sprintf("dependency %q has no readiness summary", dependency), "regenerate context")
		}
		raw, _ := json.Marshal(row)
		items = append(items, inlineItem("dependency_summary", "task:"+dependency, plan.Location{}, raw))
	}
	return items, nil
}

func selectedPlanTask(tasks plan.Tasks, taskID string) (plan.Task, error) {
	var selected *plan.Task
	for index := range tasks.Tasks {
		if tasks.Tasks[index].ID != taskID {
			continue
		}
		if selected != nil || !tasks.Tasks[index].Valid {
			return plan.Task{}, manifestFailure("context_task_invalid",
				fmt.Sprintf("task %q is duplicated or invalid", taskID), "revise plan then reapprove")
		}
		selected = &tasks.Tasks[index]
	}
	if selected == nil {
		return plan.Task{}, manifestFailure("context_task_invalid",
			fmt.Sprintf("task %q is absent", taskID), "revise plan then reapprove")
	}
	return *selected, nil
}

func validateReadinessFacts(tasks plan.Tasks, facts []core.TaskReadiness) ([]string, error) {
	if len(facts) != len(tasks.Tasks) {
		return nil, manifestFailure("context_snapshot_mismatch",
			"readiness facts do not cover every canonical task", "regenerate context")
	}
	frontier := make([]string, 0, len(facts))
	for index, fact := range facts {
		if fact.ID != tasks.Tasks[index].ID {
			return nil, manifestFailure("context_snapshot_mismatch",
				"readiness facts do not preserve canonical authored order", "regenerate context")
		}
		if fact.Readiness == core.ReadinessReady {
			frontier = append(frontier, fact.ID)
		}
	}
	return frontier, nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func outputRefs(task plan.Task) []SourceRef {
	refs := make([]SourceRef, len(task.Files))
	for index, path := range task.Files {
		refs[index] = SourceRef{Path: path, Location: task.Location}
	}
	return refs
}

func inlineItem(kind, path string, location plan.Location, raw []byte) ManifestItem {
	sum := sha256.Sum256(raw)
	location.Path = path
	return ManifestItem{
		Kind: kind, Path: path, Lane: LaneRequiredInput, Location: location,
		Digest: hex.EncodeToString(sum[:]), Content: string(raw),
	}
}

func manifestItem(kind string, source ResolvedSource) ManifestItem {
	return ManifestItem{
		Kind: kind, Path: source.Path, Lane: source.Lane, Selector: source.Selector,
		Location: source.Location, Digest: source.Digest, Content: string(source.Content),
	}
}

func projectRelative(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("source path is empty")
	}
	if !filepath.IsAbs(path) {
		relative, _, err := boundedPath(root, filepath.ToSlash(path))
		return relative, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source %q escapes project root", path)
	}
	return filepath.ToSlash(relative), nil
}

func digestJSON(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func manifestFailure(code, reason, next string) error {
	return &ManifestError{Code: code, Owner: "author", Reason: reason, Next: next}
}
