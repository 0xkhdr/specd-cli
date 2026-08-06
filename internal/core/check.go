package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type CheckSnapshot struct {
	root     string
	change   string
	snapshot gates.Snapshot
	registry gates.Registry
}

type CheckResult struct {
	Root            string          `json:"root"`
	Change          string          `json:"change"`
	StateRevision   uint64          `json:"stateRevision"`
	RegistryVersion string          `json:"registryVersion"`
	PolicyDigest    string          `json:"policyDigest"`
	Findings        []gates.Finding `json:"findings"`
	Success         bool            `json:"success"`
}

func DefaultPolicyDigest() string {
	digest := sha256.Sum256([]byte("specd-v2-default-policy-v1"))
	return hex.EncodeToString(digest[:])
}

// AssembleCheck performs all filesystem reads once. Evaluation and approval
// reuse the returned value snapshot without another parser or state read.
func AssembleCheck(root, change, policyDigest string) (CheckSnapshot, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return CheckSnapshot{}, err
	}
	if policyDigest != DefaultPolicyDigest() {
		return CheckSnapshot{}, failure.New(
			"check_policy", owner.Root(), "", "unsupported policy digest",
			"use the canonical default policy digest",
		)
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return CheckSnapshot{}, err
	}
	var snapshot CheckSnapshot
	err = lock.With(lockPath, func() error {
		assembled, assembleErr := assembleCheckLocked(owner, change, policyDigest)
		snapshot = assembled
		return assembleErr
	})
	if err != nil {
		return CheckSnapshot{}, err
	}
	return snapshot, nil
}

func assembleCheckLocked(owner *corepath.Owner, change, policyDigest string) (CheckSnapshot, error) {
	statePath, err := owner.ChangeState(change)
	if err != nil {
		return CheckSnapshot{}, err
	}
	raw, err := readCheckState(statePath)
	if err != nil {
		// readCheckState already refuses with the stable code and one legal
		// action; restating it here would be a second validation boundary.
		return CheckSnapshot{}, err
	}
	current, err := state.Decode(raw, change)
	if err != nil {
		// State decode already refuses with a stable code and one legal action.
		return CheckSnapshot{}, err
	}
	registry, err := gates.PlanningRegistry()
	if err != nil {
		return CheckSnapshot{}, failure.New(
			"check_registry", owner.Root(), "", err.Error(),
			"repair gate registry metadata",
		)
	}
	snapshot := gates.Snapshot{
		Plan:      plan.ParseChange(owner, change),
		Lifecycle: current.Stage, StateRevision: current.Revision,
		Transition:      gates.PlanningToApproved,
		RegistryVersion: registry.Version(), PolicyDigest: policyDigest,
	}
	return CheckSnapshot{
		root: owner.Root(), change: change, snapshot: snapshot, registry: registry,
	}, nil
}

// readCheckState is the one reader of a change's canonical state file, so it is
// also the one place that refuses an unreadable one. Every operation that loads
// state — check, status, next, and the transition paths — reaches this
// function, and a raw filesystem error escaping here reaches a caller as an
// untyped failure whose recovery action is the command that just failed.
func readCheckState(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, unreadableState(path, err.Error())
	}
	defer file.Close()
	opened, openedErr := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if openedErr != nil || pathErr != nil {
		return nil, unreadableState(path,
			fmt.Sprintf("inspect state file: %v", firstCheckError(openedErr, pathErr)))
	}
	if !opened.Mode().IsRegular() || !pathInfo.Mode().IsRegular() ||
		pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, pathInfo) {
		return nil, unreadableState(path, "state must be a regular non-symlink file")
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, unreadableState(path, err.Error())
	}
	return raw, nil
}

func unreadableState(path, reason string) error {
	return failure.New(
		"check_state", "", path, reason,
		"choose an existing change with valid state",
	)
}

func firstCheckError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func EvaluateCheck(snapshot CheckSnapshot) CheckResult {
	findings := snapshot.registry.Evaluate(snapshot.snapshot)
	return newCheckResult(snapshot, findings)
}

func RunCheck(root, change, policyDigest string) (CheckResult, error) {
	snapshot, err := AssembleCheck(root, change, policyDigest)
	if err != nil {
		return CheckResult{}, err
	}
	return EvaluateCheck(snapshot), nil
}

func newCheckResult(snapshot CheckSnapshot, findings []gates.Finding) CheckResult {
	result := CheckResult{
		Root: snapshot.root, Change: snapshot.change,
		StateRevision:   snapshot.snapshot.StateRevision,
		RegistryVersion: snapshot.registry.Version(),
		PolicyDigest:    snapshot.snapshot.PolicyDigest,
		Findings:        slices.Clone(findings), Success: true,
	}
	for _, finding := range findings {
		if finding.Severity == gates.Error {
			result.Success = false
			break
		}
	}
	return result
}
