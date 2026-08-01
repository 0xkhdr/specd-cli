package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/generate"
)

type InitResult struct {
	Root Root `json:"root"`
	// Guidance is the generated agent surface installed into the project root.
	// A fresh agent resumes from it, so adoption installs it rather than
	// leaving the file to a route only a Go caller can reach.
	Guidance string `json:"guidance"`
}

func Init(root string) (InitResult, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return InitResult{}, err
	}
	result := InitResult{Root: Root{Path: owner.Root()}}
	for _, target := range []string{owner.Managed(), owner.Specs(), owner.Changes(), owner.Archive()} {
		if err := owner.CheckWriteTarget(target); err != nil {
			return result, err
		}
		if err := os.Mkdir(target, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return result, failure.New("init_failed", owner.Root(), target, err.Error(), "repair the project path and retry initialization")
		}
		info, err := os.Lstat(target)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, failure.New("path_unsafe", owner.Root(), target, "managed directory is not a safe directory", "repair the project path and retry initialization")
		}
	}
	for target, family := range map[string]record.Family{
		owner.History(): record.FamilyHistory, owner.Evidence(): record.FamilyEvidence,
	} {
		if err := owner.CheckWriteTarget(target); err != nil {
			return result, err
		}
		if err := record.Ensure(target, family); err != nil {
			return result, failure.New("init_failed", owner.Root(), target, err.Error(), "repair the project path and retry initialization")
		}
	}
	if err := writeManagedIgnore(owner); err != nil {
		return result, err
	}
	if err := recoverTransactions(owner); err != nil {
		return result, err
	}
	// Guidance installation is idempotent: it rewrites only the managed region
	// and preserves authored bytes outside it, so re-running init on an adopted
	// root refreshes stale guidance without touching project notes.
	guidance, err := generate.Refresh(owner.Root())
	if err != nil {
		return result, err
	}
	result.Guidance = guidance.Path
	return result, nil
}

// managedIgnore keeps harness locks out of Git. Planning artifacts, state,
// history and evidence stay tracked: Git is their source of truth. Locks carry
// no truth and are recreated on demand.
const managedIgnore = "# Harness locks are runtime only; every other managed file is tracked truth.\n.root.lock\n.records.lock\nchanges/*/.lock\n"

func writeManagedIgnore(owner *corepath.Owner) error {
	target := filepath.Join(owner.Managed(), ".gitignore")
	if err := owner.CheckWriteTarget(target); err != nil {
		return err
	}
	// Authored bytes win: initialization is idempotent and non-destructive.
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure.New("init_failed", owner.Root(), target, err.Error(), "repair the project path and retry initialization")
	}
	if err := persist.AtomicReplace(target, []byte(managedIgnore), nil); err != nil {
		return failure.New("init_failed", owner.Root(), target, err.Error(), "repair the project path and retry initialization")
	}
	return nil
}
