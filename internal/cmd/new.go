package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

type NewResult struct {
	Root Root `json:"root"`
	state.Projection
}

// New creates a change with its scaffolded planning artifacts. The capability
// names the one delta spec to scaffold; the CLI defaults it to the change name,
// which is already a valid segment. An empty capability scaffolds no delta.
func New(root, change, actor, capability string) (NewResult, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return NewResult{}, err
	}
	result := NewResult{Root: Root{Path: owner.Root()}}
	if err := owner.CheckWriteTarget(owner.Managed()); err != nil {
		return result, err
	}
	if capability != "" {
		if err := corepath.ValidateSegment(capability); err != nil {
			return result, err
		}
	}
	if actor == "" {
		actor = "unknown"
	}
	err = lock.With(owner.RootLock(), func() error {
		if err := recoverTransactionsLocked(owner); err != nil {
			return err
		}
		if _, err := os.Stat(owner.Changes()); err != nil {
			return failure.New("not_initialized", owner.Root(), owner.Managed(), err.Error(), "initialize the project")
		}
		if err := owner.EnsureUniqueChange(change); err != nil {
			return err
		}
		created, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindCreated, Change: change,
			ExpectedRevision: record.Revision(0), ResultingRevision: record.Revision(1),
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Actor: actor,
			Payload: json.RawMessage(`{}`),
		})
		if err != nil {
			return err
		}
		initial := state.Initial(change, created.ID)
		encoded, err := state.Encode(initial)
		if err != nil {
			return err
		}
		transaction := creationTransaction{Change: change, Record: created}
		stage := transactionStage(owner, change)
		if err := owner.CheckWriteTarget(stage); err != nil {
			return err
		}
		if err := os.Mkdir(stage, 0o700); err != nil {
			// Recovery already removed any journal-less staging directory, so a
			// failure here is a real path problem, not a resumable transaction.
			return failure.New("staging_unavailable", owner.Root(), stage, err.Error(),
				"repair the staging path and retry the command")
		}
		rollback := true
		defer func() {
			if rollback {
				_ = os.RemoveAll(stage)
				_ = os.Remove(transactionPath(owner, change))
			}
		}()
		if err := persist.AtomicReplace(filepath.Join(stage, "state.json"), encoded, nil); err != nil {
			return err
		}
		// Artifacts land in the staging directory so one rename publishes state
		// and planning artifacts together.
		if _, err := plan.WriteArtifacts(stage, capability); err != nil {
			return failure.New("scaffold_failed", owner.Root(), stage, err.Error(),
				"repair the staging path and retry the command")
		}
		if err := writeTransaction(owner, transaction); err != nil {
			return err
		}
		if err := finishTransaction(owner, transaction); err != nil {
			return err
		}
		rollback = false
		result.Projection = initial.Project()
		return nil
	})
	return result, err
}

func removeDurable(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return persist.SyncDir(filepath.Dir(path))
}
