package core

import (
	"bytes"
	"cmp"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type ScopeResult struct {
	AttemptID     string   `json:"attempt_id"`
	BaselineHEAD  string   `json:"baseline_head"`
	ChangedPaths  []string `json:"changed_paths"`
	DeclaredFiles []string `json:"declared_files"`
	Assurance     string   `json:"assurance"`
	Valid         bool     `json:"valid"`
}

type gitScopeChange struct {
	Kind, Path, PreviousPath string
}

func CheckScope(root, change, taskID, attemptID string) (ScopeResult, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return ScopeResult{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return ScopeResult{}, err
	}
	var result ScopeResult
	err = lock.With(lockPath, func() error {
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		raw, err := readCheckState(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(raw, change)
		if err != nil {
			return scopeFailure("scope_state", statePath, err.Error(), "run status and repair state")
		}
		attempt, exists, err := CurrentAttempt(current, taskID)
		if err != nil {
			return scopeFailure("scope_attempt", statePath, err.Error(), "regenerate task context")
		}
		if !exists {
			return scopeFailure("scope_attempt", statePath, "state has no active attempt", "regenerate task context")
		}
		if attempt.Change != change || attempt.ID != attemptID || attempt.TaskID != taskID {
			return scopeFailure(
				"scope_attempt", statePath, "active attempt identity is missing or mismatched",
				"regenerate task context",
			)
		}
		if current.Revision != attempt.RevisionAfter {
			return scopeFailure(
				"scope_drift", statePath, "state revision no longer matches attempt",
				"run status and regenerate task context",
			)
		}
		if attemptStateGuardHash(current, taskID) != attempt.StateGuardHash {
			return scopeFailure(
				"scope_drift", statePath, "state outside the attempt-owned fields changed",
				"run status and regenerate task context",
			)
		}
		activity, err := ProjectTaskActivity(current.Tasks, attempt.TaskID)
		if err != nil || activity != TaskInProgress {
			return scopeFailure(
				"scope_drift", statePath, "attempt task is not active",
				"run status and regenerate task context",
			)
		}
		if err := validateAttemptHistory(owner, current, attempt); err != nil {
			return err
		}

		changePlan := plan.ParseChange(owner, change)
		if _, err := BuildTaskGraph(changePlan.Tasks); err != nil {
			return err
		}
		task, found := canonicalTask(changePlan.Tasks.Tasks, attempt.TaskID)
		if !found {
			return scopeFailure(
				"scope_drift", changePlan.Tasks.Source.Path, "attempt task is no longer canonical",
				"revise the plan and obtain fresh human approval",
			)
		}
		files, err := validateAttemptFiles(owner.Root(), task.Files)
		if err != nil {
			return err
		}
		if taskContractHash(task) != attempt.ContractHash || !equalStrings(files, attempt.DeclaredFiles) {
			return scopeFailure(
				"scope_drift", changePlan.Tasks.Source.Path, "task contract no longer matches attempt",
				"revise the plan and obtain fresh human approval",
			)
		}
		registry, err := gates.PlanningRegistry()
		if err != nil {
			return err
		}
		approval, err := projectApprovalStatusLocked(owner, change, registry.Version(), DefaultPolicyDigest())
		if err != nil {
			return err
		}
		if !approval.Current || approval.Approval == nil ||
			approval.Approval.AggregateHash != attempt.ApprovalHash {
			return scopeFailure(
				"scope_drift", statePath, "human approval no longer matches attempt",
				"rerun check, then obtain fresh human approval",
			)
		}
		head, err := gitOutput(owner.Root(), "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil || head != attempt.BaselineHEAD {
			return scopeFailure(
				"scope_drift", owner.Root(), "Git HEAD no longer matches attempt baseline",
				"regenerate task context at the current commit",
			)
		}

		changes, err := deriveScopeChanges(owner.Root(), attempt.BaselineHEAD)
		if err != nil {
			return err
		}
		declared := make(map[string]bool, len(attempt.DeclaredFiles))
		for _, file := range attempt.DeclaredFiles {
			declared[file] = true
		}
		changed := make([]string, 0, len(changes))
		offending := make([]string, 0)
		managedAllowed, err := validatedAttemptManagedPaths(owner, change)
		if err != nil {
			return err
		}
		for _, item := range changes {
			if item.Kind != "A" && item.Kind != "M" {
				offending = append(offending, itemPaths(item)...)
				continue
			}
			for _, changedPath := range itemPaths(item) {
				if managedLockPath(changedPath) {
					continue
				}
				if managedScopePath(changedPath) {
					if managedAllowed[changedPath] {
						continue
					}
					offending = append(offending, changedPath)
					continue
				}
				if !declared[changedPath] {
					offending = append(offending, changedPath)
					continue
				}
				if err := rejectScopeSymlink(owner.Root(), changedPath); err != nil {
					return err
				}
				if err := rejectManagedAlias(owner.Root(), changedPath); err != nil {
					return err
				}
				changed = append(changed, changedPath)
			}
		}
		changed = sortedUnique(changed)
		offending = sortedUnique(offending)
		if len(offending) != 0 {
			return scopeFailure(
				"scope_outside", strings.Join(offending, ", "),
				"advisory scope check did not contain the process; changed paths exceed exact attempt authority: "+
					strings.Join(offending, ", "),
				"revise the plan and obtain fresh human approval",
			)
		}
		result = ScopeResult{
			AttemptID: attempt.ID, BaselineHEAD: attempt.BaselineHEAD,
			ChangedPaths: changed, DeclaredFiles: slices.Clone(attempt.DeclaredFiles),
			Assurance: AttemptAssurance, Valid: true,
		}
		return nil
	})
	return result, err
}

func validatedAttemptManagedPaths(owner *corepath.Owner, change string) (map[string]bool, error) {
	statePath, err := owner.ChangeState(change)
	if err != nil {
		return nil, err
	}
	stateRelative, _ := filepath.Rel(owner.Root(), statePath)
	historyRelative, _ := filepath.Rel(owner.Root(), owner.History())
	evidenceRelative, _ := filepath.Rel(owner.Root(), owner.Evidence())
	if _, diagnostics, err := record.Replay(owner.Evidence(), record.FamilyEvidence); err != nil {
		return nil, err
	} else if len(diagnostics) != 0 {
		return nil, scopeFailure("scope_history", owner.Evidence(),
			"evidence has an incomplete tail", "repair evidence and run status")
	}
	return map[string]bool{
		filepath.ToSlash(stateRelative):    true,
		filepath.ToSlash(historyRelative):  true,
		filepath.ToSlash(evidenceRelative): true,
	}, nil
}

func validateAttemptHistory(owner *corepath.Owner, current state.State, attempt Attempt) error {
	records, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return scopeFailure("scope_history", owner.History(), "history has an incomplete tail", "repair history and run status")
	}
	found := false
	for _, item := range records {
		if found && item.Change == attempt.Change {
			return scopeFailure(
				"scope_history", owner.History(), "attempt is not the latest transition for its change",
				"run status and repair attempt history",
			)
		}
		if item.ID != current.LastTransition {
			continue
		}
		if item.Kind != record.KindAttempt || item.Change != attempt.Change ||
			item.Actor != attempt.Actor || item.ExpectedRevision == nil ||
			item.ResultingRevision == nil ||
			*item.ExpectedRevision != attempt.RevisionBefore ||
			*item.ResultingRevision != attempt.RevisionAfter {
			break
		}
		recorded, decodeErr := decodeAttempt(item.Payload)
		if decodeErr == nil && recorded.ID == attempt.ID {
			found = true
			continue
		}
		break
	}
	if found {
		return nil
	}
	return scopeFailure(
		"scope_history", owner.History(), "attempt state is not bound to exact history",
		"run status and repair attempt history",
	)
}

func deriveScopeChanges(root, baseline string) ([]gitScopeChange, error) {
	raw, err := gitRaw(root, "diff", "--name-status", "-z", "--find-renames", baseline, "--")
	if err != nil {
		return nil, scopeFailure("scope_git", root, err.Error(), "repair Git state and regenerate task context")
	}
	changes, err := parseGitNameStatus(raw)
	if err != nil {
		return nil, scopeFailure("scope_git", root, err.Error(), "repair Git state and regenerate task context")
	}
	untracked, err := gitRaw(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, scopeFailure("scope_git", root, err.Error(), "repair Git state and regenerate task context")
	}
	for _, rawPath := range bytes.Split(untracked, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}
		normalized, err := normalizeScopePath(rawPath)
		if err != nil {
			return nil, err
		}
		changes = append(changes, gitScopeChange{Kind: "A", Path: normalized})
	}
	ignored, err := gitRaw(root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, scopeFailure("scope_git", root, err.Error(), "repair Git state and regenerate task context")
	}
	for _, rawPath := range bytes.Split(ignored, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}
		normalized, err := normalizeScopePath(rawPath)
		if err != nil {
			return nil, err
		}
		changes = append(changes, gitScopeChange{Kind: "A", Path: normalized})
	}
	slices.SortStableFunc(changes, func(a, b gitScopeChange) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.PreviousPath, b.PreviousPath))
	})
	return changes, nil
}

func parseGitNameStatus(raw []byte) ([]gitScopeChange, error) {
	if len(raw) == 0 {
		return []gitScopeChange{}, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("malformed Git name-status output")
	}
	fields := bytes.Split(raw[:len(raw)-1], []byte{0})
	var changes []gitScopeChange
	for index := 0; index < len(fields); {
		status := string(fields[index])
		index++
		if !validGitScopeStatus(status) {
			return nil, errors.New("malformed Git change status")
		}
		kind := status[:1]
		if index >= len(fields) {
			return nil, errors.New("malformed Git changed path")
		}
		first, err := normalizeScopePath(fields[index])
		if err != nil {
			return nil, err
		}
		index++
		change := gitScopeChange{Kind: kind, Path: first}
		if kind == "R" || kind == "C" {
			if index >= len(fields) {
				return nil, errors.New("malformed Git rename paths")
			}
			change.PreviousPath = first
			change.Path, err = normalizeScopePath(fields[index])
			if err != nil {
				return nil, err
			}
			index++
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func validGitScopeStatus(status string) bool {
	if status == "" {
		return false
	}
	kind := status[:1]
	if kind != "R" && kind != "C" {
		return len(status) == 1 && strings.Contains("AMDTUXB", kind)
	}
	if len(status) == 1 {
		return false
	}
	for _, character := range status[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func normalizeScopePath(raw []byte) (string, error) {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return "", scopeFailure("scope_path", "", "Git path is empty or invalid UTF-8", "repair Git paths and regenerate task context")
	}
	value := string(raw)
	if strings.Contains(value, "\\") || strings.HasPrefix(value, "/") ||
		path.Clean(value) != value || value == "." || value == ".." ||
		strings.HasPrefix(value, "../") {
		return "", scopeFailure("scope_path", value, "Git path is unsafe", "repair Git paths and regenerate task context")
	}
	return value, nil
}

func rejectScopeSymlink(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return scopeFailure("scope_path", relative, err.Error(), "repair the path and regenerate task context")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return scopeFailure(
				"scope_path", relative, "changed path traverses a symlink",
				"replace the symlink and regenerate task context",
			)
		}
	}
	return nil
}

func managedScopePath(value string) bool {
	return value == ".specd" || strings.HasPrefix(value, ".specd/") ||
		value == ".git" || strings.HasPrefix(value, ".git/")
}

// managedLockPath reports a D9 harness lock. Locks are mutual exclusion, never
// content: they hold no truth, any operation may create one, and Git reports
// them as untracked or ignored depending on whether the managed tree is
// committed. Treating one as an out-of-scope write would refuse completion for
// work that never touched it.
func managedLockPath(value string) bool {
	return managedScopePath(value) && strings.HasSuffix(value, ".lock")
}

func itemPaths(item gitScopeChange) []string {
	if item.PreviousPath == "" {
		return []string{item.Path}
	}
	return []string{item.PreviousPath, item.Path}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedUnique(values []string) []string {
	slices.Sort(values)
	result := values[:0]
	for index, value := range values {
		if index == 0 || value != values[index-1] {
			result = append(result, value)
		}
	}
	return result
}

func scopeFailure(code, path, reason, next string) error {
	return attemptFailure(code, path, reason, next)
}
