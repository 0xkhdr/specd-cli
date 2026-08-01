package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const (
	attemptExtensionKey = "attempts"
	AttemptAssurance    = "advisory"
)

type AttemptRequest struct {
	TaskID           string
	Authority        TaskTransitionAuthority
	ExpectedRevision uint64
	AfterHistory     func() error
}

type Attempt = record.AttemptPayload

type StartAttemptIntent struct {
	TaskID           string
	ExpectedRevision uint64
	Actor            string
}

// StartTaskAttempt is the harness-owned admission route. It keeps authority
// construction inside core while exposing one explicit, revision-checked
// operation to command adapters.
func StartTaskAttempt(root, change string, intent StartAttemptIntent) (Attempt, error) {
	return StartAttempt(root, change, AttemptRequest{
		TaskID: intent.TaskID, ExpectedRevision: intent.ExpectedRevision,
		Authority: trustedTaskTransitionAuthority(intent.Actor),
	})
}

func StartAttempt(root, change string, request AttemptRequest) (Attempt, error) {
	actor := strings.TrimSpace(request.Authority.actor)
	if actor == "" {
		return Attempt{}, attemptFailure(
			"attempt_unauthorized", "", "attempt actor is unauthorized",
			"request context through an authorized harness operation",
		)
	}
	owner, err := corepath.New(root)
	if err != nil {
		return Attempt{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return Attempt{}, err
	}
	var started Attempt
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
			return attemptFailure("plan_invalid", statePath, err.Error(), "run status and repair state")
		}
		if current.Revision != request.ExpectedRevision {
			return attemptFailure(
				"stale_revision", statePath,
				fmt.Sprintf("expected revision %d, current %d", request.ExpectedRevision, current.Revision),
				fmt.Sprintf("run status and retry from revision %d", current.Revision),
			)
		}
		attempts, err := decodeAttempts(current.Extensions[attemptExtensionKey])
		if err != nil {
			return attemptFailure("plan_invalid", statePath, err.Error(), "run status and repair state")
		}
		if _, exists := attempts[request.TaskID]; exists {
			return attemptFailure(
				"attempt_exists", statePath, "task already has a bound attempt",
				"run status and continue the active attempt",
			)
		}
		for taskID := range attempts {
			activity, activityErr := ProjectTaskActivity(current.Tasks, taskID)
			if activityErr != nil {
				return activityErr
			}
			if activity != TaskCompleted {
				return attemptFailure(
					"attempt_exists", statePath,
					fmt.Sprintf("task %q has a non-terminal bound attempt", taskID),
					"run status and finish the active attempt",
				)
			}
		}

		changePlan := plan.ParseChange(owner, change)
		_, err = BuildTaskGraph(changePlan.Tasks)
		if err != nil {
			return err
		}
		task, found := canonicalTask(changePlan.Tasks.Tasks, request.TaskID)
		if !found {
			return attemptFailure(
				"task_unknown", changePlan.Tasks.Source.Path, fmt.Sprintf("task %q is not canonical", request.TaskID),
				"choose a task reported by status",
			)
		}
		files, err := validateAttemptFiles(owner.Root(), task.Files)
		if err != nil {
			return err
		}
		registry, err := gates.PlanningRegistry()
		if err != nil {
			return err
		}
		approval, err := projectApprovalStatusLocked(owner, change, registry.Version(), DefaultPolicyDigest())
		if err != nil {
			return err
		}
		readiness := ProjectReadiness(changePlan.Tasks, current.Tasks, current.Stage, approval)
		if !readiness.InFrontier(request.TaskID) {
			reason := fmt.Sprintf("task %q is not in the current frontier", request.TaskID)
			if row, ok := readiness.Task(request.TaskID); ok && row.Blocker != nil {
				reason += ": " + row.Blocker.Code
			}
			if approval.Reason != "" {
				reason += ": " + approval.Reason
			}
			return attemptFailure(
				"attempt_not_ready", statePath, reason,
				"run status and choose a ready task",
			)
		}
		if approval.Approval == nil || !approval.Current {
			return attemptFailure(
				"approval_stale", statePath, "attempt requires current human approval",
				"rerun check, then obtain fresh human approval",
			)
		}
		activity, err := ProjectTaskActivity(current.Tasks, request.TaskID)
		if err != nil || !CanTransitionTaskActivity(activity, TaskInProgress) {
			return attemptFailure(
				"attempt_transition", statePath, fmt.Sprintf("task %q cannot start from %q", request.TaskID, activity),
				"run status and choose a pending ready task",
			)
		}
		history, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
		if err != nil {
			return err
		}
		if len(diagnostics) != 0 {
			return attemptFailure(
				"attempt_history", owner.History(), "history has an incomplete tail",
				"repair history and run status",
			)
		}
		prior, recoveryID, recoveryFound, err := recoverableAttempt(history, change, current.Revision)
		if err != nil {
			return err
		}
		allowedDirty := map[string]bool{}
		if recoveryFound {
			allowedDirty[".specd/history.jsonl"] = true
		}
		head, err := cleanGitBaseline(owner.Root(), allowedDirty)
		if err != nil {
			return err
		}
		started, err = record.NewAttemptPayload(Attempt{
			Change: change, TaskID: request.TaskID, BaselineHEAD: head,
			RevisionBefore: current.Revision, RevisionAfter: current.Revision + 1,
			ContractHash: taskContractHash(task), ApprovalHash: approval.Approval.AggregateHash,
			StateGuardHash: attemptStateGuardHash(current, request.TaskID),
			DeclaredFiles:  files, Actor: actor, Assurance: AttemptAssurance,
			ActivityFrom: string(TaskPending), ActivityTo: string(TaskInProgress),
		})
		if err != nil {
			return err
		}

		if recoveryFound {
			if prior.ID != started.ID || historyActor(history, recoveryID) != actor {
				return attemptFailure(
					"attempt_recovery", owner.History(), "pending attempt differs from current request or inputs",
					"run status to recover the pending attempt",
				)
			}
			started = prior
			return persistAttemptState(statePath, current, attempts, prior, recoveryID)
		}

		payload, _ := json.Marshal(started)
		historyRecord, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindAttempt, Change: change,
			ExpectedRevision:  record.Revision(current.Revision),
			ResultingRevision: record.Revision(current.Revision + 1),
			Timestamp:         time.Now().UTC().Format(time.RFC3339Nano),
			Actor:             started.Actor, Payload: payload,
		})
		if err != nil {
			return err
		}
		if err := record.Append(owner.History(), record.FamilyHistory, historyRecord); err != nil {
			return err
		}
		if request.AfterHistory != nil {
			if err := request.AfterHistory(); err != nil {
				return attemptFailure(
					"attempt_interrupted", owner.History(), err.Error(),
					"run status to recover the pending attempt",
				)
			}
		}
		return persistAttemptState(statePath, current, attempts, started, historyRecord.ID)
	})
	return started, err
}

func attemptStateGuardHash(current state.State, taskID string) string {
	tasks := make(map[string]json.RawMessage, len(current.Tasks))
	for id, raw := range current.Tasks {
		tasks[id] = append(json.RawMessage(nil), raw...)
	}
	current.Tasks = tasks
	extensions := make(map[string]json.RawMessage, len(current.Extensions))
	for key, raw := range current.Extensions {
		extensions[key] = append(json.RawMessage(nil), raw...)
	}
	current.Extensions = extensions
	current.Revision = 0
	current.LastTransition = ""
	delete(current.Tasks, taskID)
	if current.Extensions != nil {
		attempts, _ := decodeAttempts(current.Extensions[attemptExtensionKey])
		delete(attempts, taskID)
		if len(attempts) == 0 {
			delete(current.Extensions, attemptExtensionKey)
		} else {
			raw, _ := json.Marshal(attempts)
			current.Extensions[attemptExtensionKey] = raw
		}
	}
	raw, _ := json.Marshal(current)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func canonicalTask(tasks []plan.Task, id string) (plan.Task, bool) {
	for _, task := range tasks {
		if task.ID == id {
			return task, true
		}
	}
	return plan.Task{}, false
}

func validateAttemptFiles(root string, declared []string) ([]string, error) {
	files := append([]string(nil), declared...)
	sort.Strings(files)
	for index, file := range files {
		if file == ".specd" || strings.HasPrefix(file, ".specd/") ||
			file == ".git" || strings.HasPrefix(file, ".git/") {
			return nil, attemptFailure(
				"attempt_scope", file, "managed harness or Git paths cannot be task write authority",
				"remove managed paths from the task and obtain fresh approval",
			)
		}
		if index > 0 && files[index-1] == file {
			return nil, attemptFailure(
				"attempt_scope", file, "declared task file is duplicated",
				"deduplicate task files and obtain fresh approval",
			)
		}
		if err := rejectAttemptSymlink(root, file); err != nil {
			return nil, err
		}
		if err := rejectManagedAlias(root, file); err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, attemptFailure(
			"attempt_scope", "", "task has no declared implementation files",
			"declare exact task files and obtain fresh approval",
		)
	}
	return files, nil
}

func rejectManagedAlias(root, relative string) error {
	target := filepath.Join(root, filepath.FromSlash(relative))
	targetInfo, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !targetInfo.Mode().IsRegular() {
		return attemptFailure("attempt_scope", relative,
			"existing declared path must be one regular file", "repair the declared path and rerun check")
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		return attemptFailure("attempt_scope", relative,
			"declared task file traverses a symlink", "replace the symlink and obtain fresh approval")
	}
	for _, managed := range []string{filepath.Join(root, ".specd"), filepath.Join(root, ".git")} {
		err := filepath.WalkDir(managed, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if os.SameFile(targetInfo, info) {
				return errors.New("declared path aliases managed harness or Git content")
			}
			return nil
		})
		if err != nil {
			return attemptFailure("attempt_scope", relative, err.Error(),
				"replace the alias with an independent file and obtain fresh approval")
		}
	}
	return nil
}

func rejectAttemptSymlink(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return attemptFailure("attempt_scope", relative, err.Error(), "repair the declared path and rerun check")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return attemptFailure(
				"attempt_scope", relative, "declared task file traverses a symlink",
				"replace the symlink with an exact repository file and obtain fresh approval",
			)
		}
	}
	return nil
}

func cleanGitBaseline(root string, allowedDirty map[string]bool) (string, error) {
	top, err := gitOutput(root, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(top) != filepath.Clean(root) {
		return "", attemptFailure(
			"attempt_git", root, "project root is not a resolvable Git worktree root",
			"initialize and commit the project at its selected root",
		)
	}
	head, err := gitOutput(root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || len(head) < 40 {
		return "", attemptFailure(
			"attempt_commitless", root, "Git HEAD does not resolve to a commit",
			"commit the project before starting execution",
		)
	}
	tracked, err := gitRaw(root, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return "", attemptFailure("attempt_git", root, err.Error(), "repair the Git worktree and run status")
	}
	untracked, err := gitRaw(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", attemptFailure("attempt_git", root, err.Error(), "repair the Git worktree and run status")
	}
	var dirty []string
	for _, raw := range append(bytes.Split(tracked, []byte{0}), bytes.Split(untracked, []byte{0})...) {
		path := filepath.ToSlash(string(raw))
		if path != "" && !allowedDirty[path] && !attemptManagedPath(path) {
			dirty = append(dirty, path)
		}
	}
	if len(dirty) != 0 {
		return "", attemptFailure(
			"attempt_dirty", root, "Git worktree is not clean: "+strings.Join(dirty, ", "),
			"make the Git worktree clean, then retry",
		)
	}
	return head, nil
}

func attemptManagedPath(value string) bool {
	return value == ".specd" || strings.HasPrefix(value, ".specd/")
}

func gitOutput(root string, arguments ...string) (string, error) {
	raw, err := gitRaw(root, arguments...)
	return strings.TrimSpace(string(raw)), err
}

func gitRaw(root string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	return command.Output()
}

// TaskContractHash is the canonical identity of an authored task contract.
// Callers must use this instead of maintaining a second hashing algorithm.
func TaskContractHash(task plan.Task) string {
	references := make([][2]string, len(task.References))
	for index, reference := range task.References {
		references[index] = [2]string{reference.Capability, reference.Requirement}
	}
	contract := struct {
		ID, Role, Verify, Acceptance string
		Files, DependsOn             []string
		References                   [][2]string
	}{
		task.ID, task.Role, task.Verify, task.Acceptance,
		append([]string(nil), task.Files...), append([]string(nil), task.DependsOn...), references,
	}
	raw, _ := json.Marshal(contract)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func taskContractHash(task plan.Task) string {
	return TaskContractHash(task)
}

func recoverableAttempt(records []record.Record, change string, revision uint64) (Attempt, string, bool, error) {
	for index := len(records) - 1; index >= 0; index-- {
		item := records[index]
		if item.Change != change || item.Kind != record.KindAttempt ||
			item.ExpectedRevision == nil || item.ResultingRevision == nil ||
			*item.ExpectedRevision != revision || *item.ResultingRevision != revision+1 {
			continue
		}
		attempt, err := decodeAttempt(item.Payload)
		if err != nil || attempt.Change != item.Change ||
			attempt.RevisionBefore != *item.ExpectedRevision ||
			attempt.RevisionAfter != *item.ResultingRevision ||
			attempt.Actor != item.Actor {
			return Attempt{}, "", false, attemptFailure(
				"attempt_history", "", "attempt history envelope does not match payload",
				"repair history and run status",
			)
		}
		return attempt, item.ID, true, nil
	}
	return Attempt{}, "", false, nil
}

func decodeAttempt(raw json.RawMessage) (Attempt, error) {
	return record.DecodeAttemptPayload(raw)
}

func decodeAttempts(raw json.RawMessage) (map[string]Attempt, error) {
	if len(raw) == 0 {
		return map[string]Attempt{}, nil
	}
	var attempts map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&attempts); err != nil {
		return nil, err
	}
	result := make(map[string]Attempt, len(attempts))
	for taskID, encoded := range attempts {
		attempt, err := decodeAttempt(encoded)
		if err != nil || attempt.TaskID != taskID {
			return nil, errors.New("persisted attempt map is malformed")
		}
		result[taskID] = attempt
	}
	return result, nil
}

func CurrentAttempt(current state.State, taskID string) (Attempt, bool, error) {
	attempts, err := decodeAttempts(current.Extensions[attemptExtensionKey])
	if err != nil {
		return Attempt{}, false, err
	}
	attempt, found := attempts[taskID]
	return attempt, found, nil
}

func persistAttemptState(path string, current state.State, attempts map[string]Attempt, attempt Attempt, historyID string) error {
	attempts[attempt.TaskID] = attempt
	raw, err := json.Marshal(attempts)
	if err != nil {
		return err
	}
	if current.Extensions == nil {
		current.Extensions = map[string]json.RawMessage{}
	}
	current.Extensions[attemptExtensionKey] = raw
	current.Tasks[attempt.TaskID] = json.RawMessage(`"in_progress"`)
	current.Revision++
	current.LastTransition = historyID
	encoded, err := state.Encode(current)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(path, encoded, nil)
}

func attemptFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}
