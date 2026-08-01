package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
)

type Mutator func(*State) error

// Mutate takes only the change lock. Callers needing a root lock must acquire
// it first: root before change is the global lock order.
//
// path is <change>/state.json, so the one change lock is <change>/.lock, the
// same file corepath.Owner.ChangeLock names and every other state writer takes.
func Mutate(path, expectedChange string, expectedRevision uint64, mutate Mutator) (State, error) {
	var next State
	err := lock.With(filepath.Join(filepath.Dir(path), ".lock"), func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read state: %w", err)
		}
		current, err := Decode(data, expectedChange)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return failure.New("stale_revision", "", path,
				fmt.Sprintf("expected %d, current %d", expectedRevision, current.Revision),
				fmt.Sprintf("retry from revision %d", current.Revision))
		}
		next = current
		if err := mutate(&next); err != nil {
			return err
		}
		next.Revision++
		if next.Change != current.Change || next.SchemaVersion != current.SchemaVersion {
			return errors.New("mutation cannot change state identity or schema")
		}
		encoded, err := Encode(next)
		if err != nil {
			return err
		}
		return persist.AtomicReplace(path, encoded, nil)
	})
	return next, err
}
