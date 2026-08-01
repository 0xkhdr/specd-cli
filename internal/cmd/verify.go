package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type VerifyOptions struct {
	Actor       string
	Timeout     time.Duration
	OutputLimit int
	// Production options. An empty class keeps the default test-run loop, which
	// runs exactly the approved task verification command.
	Profile      string
	Class        string
	CheckID      string
	Command      string
	Reviewer     string
	ReviewPassed bool
}

type VerifyResult struct {
	Evidence evidence.TestRun `json:"evidence"`
	// Production carries build, lint, and review observations. It is absent for
	// the default profile, whose result shape is unchanged.
	Production *evidence.Production `json:"production,omitempty"`
	RecordID   string               `json:"recordId"`
	Complete   bool                 `json:"complete"`
}

// verifyBinding is the exact canonical identity one observation is recorded
// against. It is assembled once and reused by every evidence class.
type verifyBinding struct {
	snapshot core.ReadinessSnapshot
	task     plan.Task
	scope    core.ScopeResult
	approval string
}

func Verify(ctx context.Context, root, change, taskID, attemptID string, options VerifyOptions) (VerifyResult, error) {
	if strings.TrimSpace(options.Actor) == "" {
		return VerifyResult{}, errors.New("verify actor is required; next: retry through an authorized harness operation")
	}
	binding, err := bindVerification(root, change, taskID, attemptID)
	if err != nil {
		return VerifyResult{}, err
	}
	if class := strings.TrimSpace(options.Class); class != "" && class != evidence.ClassTestRun {
		return verifyProduction(ctx, root, change, taskID, attemptID, options, binding)
	}
	if options.CheckID != "" || options.Command != "" || options.Reviewer != "" ||
		strings.TrimSpace(options.Profile) == string(core.ProfileProduction) {
		return VerifyResult{}, errors.New(
			"production verification requires an explicit evidence class; next: pass the build, lint, or review class")
	}
	request, err := verifyexec.ParseCommand(binding.task.Verify)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify command: %w; next: repair the plan and reapprove", err)
	}
	request.Dir, request.Timeout, request.OutputLimit = binding.snapshot.Root(), options.Timeout, options.OutputLimit
	commandHash, err := verifyexec.CommandDigest(request)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify command: %w; next: repair the plan and reapprove", err)
	}
	// Check once more after constructing the exact request. This is the final
	// authority check before process creation.
	if _, err = core.CheckScope(root, change, taskID, attemptID); err != nil {
		return VerifyResult{}, err
	}
	run, err := verifyexec.Run(ctx, request)
	if err != nil {
		return VerifyResult{}, err
	}
	if run.CommandDigest != commandHash {
		return VerifyResult{}, errors.New("verification command identity changed before persistence")
	}
	subject := evidence.Subject{
		Change: change, TaskID: taskID, AttemptID: attemptID, HEAD: binding.scope.BaselineHEAD,
		TaskHash: core.TaskContractHash(binding.task), CommandHash: commandHash,
		ApprovalHash: binding.approval, StateRevision: binding.snapshot.StateRevision(),
	}
	// A command or concurrent writer may move HEAD or invalidate authority while
	// tests run. Preserve the observation, but never persist it as a pass.
	if !verificationIdentityCurrent(root, change, taskID, attemptID, subject) {
		run.Passed = false
		run.NonVacuous = false
	}
	observation, err := evidence.NewTestRun(subject, run)
	if err != nil {
		return VerifyResult{}, err
	}
	owner, err := corepath.New(root)
	if err != nil {
		return VerifyResult{}, err
	}
	item, err := evidence.Append(owner.Evidence(), options.Actor, observation)
	if err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{Evidence: observation, RecordID: item.ID, Complete: false}, nil
}

// verifyProduction records one build, lint, or review observation. Build and
// lint reuse the sole stage-5 runner; review executes nothing and is authored
// by a reviewer who is never the recording actor.
func verifyProduction(ctx context.Context, root, change, taskID, attemptID string, options VerifyOptions, binding verifyBinding) (VerifyResult, error) {
	policy, err := core.ResolveProfile(options.Profile)
	if err != nil {
		return VerifyResult{}, err
	}
	if !policy.Production() {
		return VerifyResult{}, errors.New(
			"production evidence requires the production profile; next: select the production profile")
	}
	check := evidence.RequiredCheck{
		Class: strings.TrimSpace(options.Class), CheckID: strings.TrimSpace(options.CheckID),
	}
	declared := false
	for _, required := range policy.RequiredChecks {
		declared = declared || required == check
	}
	if !declared {
		return VerifyResult{}, fmt.Errorf(
			"check %q is not declared by policy digest %s; next: run one check the current policy declares",
			check.String(), policy.Digest())
	}
	subject := evidence.ProductionSubject{
		Subject: evidence.Subject{
			Change: change, TaskID: taskID, AttemptID: attemptID, HEAD: binding.scope.BaselineHEAD,
			TaskHash: core.TaskContractHash(binding.task), ApprovalHash: binding.approval,
			StateRevision: binding.snapshot.StateRevision(),
		},
		Check: check, PolicyDigest: policy.Digest(),
	}
	owner, err := corepath.New(root)
	if err != nil {
		return VerifyResult{}, err
	}
	var observation evidence.Production
	if evidence.Runnable(check.Class) {
		if strings.TrimSpace(options.Command) == "" {
			return VerifyResult{}, fmt.Errorf(
				"check %s requires one command; next: pass the command that proves %s",
				check.String(), check.String())
		}
		if options.Reviewer != "" {
			return VerifyResult{}, fmt.Errorf(
				"check %s is runnable; next: record a reviewer only for review proof", check.String())
		}
		request, parseErr := verifyexec.ParseCommand(options.Command)
		if parseErr != nil {
			return VerifyResult{}, fmt.Errorf("verify command: %w; next: pass one executable command", parseErr)
		}
		request.Dir = binding.snapshot.Root()
		request.Timeout, request.OutputLimit = options.Timeout, options.OutputLimit
		commandHash, digestErr := verifyexec.CommandDigest(request)
		if digestErr != nil {
			return VerifyResult{}, fmt.Errorf("verify command: %w; next: pass one executable command", digestErr)
		}
		// Final authority check before process creation, exactly as test-run.
		if _, err := core.CheckScope(root, change, taskID, attemptID); err != nil {
			return VerifyResult{}, err
		}
		run, runErr := verifyexec.Run(ctx, request)
		if runErr != nil {
			return VerifyResult{}, runErr
		}
		if run.CommandDigest != commandHash {
			return VerifyResult{}, errors.New("verification command identity changed before persistence")
		}
		subject.CommandHash = commandHash
		if !verificationIdentityCurrent(root, change, taskID, attemptID, subject.Subject) {
			run.Passed, run.NonVacuous = false, false
		}
		observation, err = evidence.NewProduction(subject, run)
	} else {
		if strings.TrimSpace(options.Command) != "" {
			return VerifyResult{}, fmt.Errorf(
				"check %s never executes a command; next: record the reviewer verdict instead", check.String())
		}
		if !verificationIdentityCurrent(root, change, taskID, attemptID, subject.Subject) {
			return VerifyResult{}, errors.New(
				"review inputs are stale; next: regenerate task context at current HEAD")
		}
		observation, err = evidence.NewReview(subject, options.Reviewer, options.ReviewPassed, time.Now())
	}
	if err != nil {
		return VerifyResult{}, fmt.Errorf("%w; next: repair the observation and rerun verify", err)
	}
	item, err := evidence.AppendProduction(owner.Evidence(), options.Actor, observation)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("%w; next: repair the observation and rerun verify", err)
	}
	return VerifyResult{Production: &observation, RecordID: item.ID, Complete: false}, nil
}

// bindVerification resolves the canonical task, scope authority, and current
// approval once. It is the shared guard sequence for every evidence class.
func bindVerification(root, change, taskID, attemptID string) (verifyBinding, error) {
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return verifyBinding{}, err
	}
	row, found := snapshot.Model().Task(taskID)
	if !found || row.Activity != core.TaskInProgress {
		return verifyBinding{}, errors.New("verify requires the active canonical task; next: regenerate task context")
	}
	if _, found := findTask(snapshot.Plan().Tasks.Tasks, taskID); !found {
		return verifyBinding{}, errors.New("verify task is not canonical; next: repair the plan and reapprove")
	}
	if _, err := core.CheckScope(root, change, taskID, attemptID); err != nil {
		return verifyBinding{}, err
	}
	// Refresh canonical inputs after the broad scope walk so the command built
	// by the caller is the one checked by the final pre-spawn scope validation.
	snapshot, err = core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return verifyBinding{}, err
	}
	row, found = snapshot.Model().Task(taskID)
	if !found || row.Activity != core.TaskInProgress {
		return verifyBinding{}, errors.New("verify requires the active canonical task; next: regenerate task context")
	}
	task, found := findTask(snapshot.Plan().Tasks.Tasks, taskID)
	if !found {
		return verifyBinding{}, errors.New("verify task is not canonical; next: repair the plan and reapprove")
	}
	scope, err := core.CheckScope(root, change, taskID, attemptID)
	if err != nil {
		return verifyBinding{}, err
	}
	approval := snapshot.Approval()
	if !scope.Valid || approval.Approval == nil || !approval.Current {
		return verifyBinding{}, errors.New("verify inputs are stale; next: obtain fresh human approval")
	}
	return verifyBinding{
		snapshot: snapshot, task: task, scope: scope,
		approval: approval.Approval.AggregateHash,
	}, nil
}

func verificationIdentityCurrent(root, change, taskID, attemptID string, subject evidence.Subject) bool {
	scope, err := core.CheckScope(root, change, taskID, attemptID)
	if err != nil || !scope.Valid || scope.BaselineHEAD != subject.HEAD {
		return false
	}
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil || snapshot.StateRevision() != subject.StateRevision {
		return false
	}
	task, found := findTask(snapshot.Plan().Tasks.Tasks, taskID)
	approval := snapshot.Approval()
	return found && core.TaskContractHash(task) == subject.TaskHash &&
		approval.Current && approval.Approval != nil &&
		approval.Approval.AggregateHash == subject.ApprovalHash
}

func findTask(tasks []plan.Task, id string) (plan.Task, bool) {
	for _, task := range tasks {
		if task.ID == id {
			return task, true
		}
	}
	return plan.Task{}, false
}
