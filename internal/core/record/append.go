package record

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
)

func Append(path string, family Family, record Record) error {
	return WithLedgerLock(path, func() error {
		return AppendUnderLock(path, family, record)
	})
}

// WithLedgerLock serializes a transaction spanning history and evidence,
// which share one ledger lock in their managed parent.
func WithLedgerLock(path string, transaction func() error) error {
	if transaction == nil {
		return refusal("record-transaction", path, 0, "transaction is required")
	}
	return lock.With(filepath.Join(filepath.Dir(path), ".records.lock"), transaction)
}

// AppendUnderLock appends without reacquiring the shared ledger lock.
// Callers must be inside WithLedgerLock.
func AppendUnderLock(path string, family Family, record Record) error {
	if record.Family != family {
		return refusal("record-family", path, 0, "record does not match file family")
	}
	if err := record.Validate(); err != nil {
		return refusal("record-invalid", path, 0, err.Error())
	}
	if err := validatePath(path, family); err != nil {
		return refusal("record-path", path, 0, err.Error())
	}

	return appendLocked(path, family, record)
}

func Ensure(path string, family Family) error {
	if err := validatePath(path, family); err != nil {
		return refusal("record-path", path, 0, err.Error())
	}
	file, err := openAppend(path)
	if err != nil {
		return refusal("record-open", path, 0, err.Error())
	}
	return file.Close()
}

func appendLocked(path string, family Family, record Record) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return refusal("record-symlink", path, 0, "record target is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return refusal("record-read", path, 0, err.Error())
	}
	records, diagnostics, err := Replay(path, family)
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return refusal("record-torn-tail", path, diagnostics[0].Line, "append refused while incomplete tail exists")
	}
	for _, prior := range records {
		if prior.ID == record.ID {
			return refusal("record-duplicate", path, 0, "duplicate record id")
		}
	}
	if record.ExpectedRevision != nil {
		for index := len(records) - 1; index >= 0; index-- {
			prior := records[index]
			if prior.Change == record.Change && prior.ResultingRevision != nil {
				if *record.ExpectedRevision != *prior.ResultingRevision {
					// History is append-only, so a deleted change directory
					// leaves its records behind and reusing the name lands
					// here with intact history. Naming only the file would
					// send that caller to repair something that is not broken.
					return failure.New("record-revision-chain", "", path,
						"non-contiguous change revision",
						"restore the change and its history from version control, "+
							"or choose a name with no recorded history")
				}
				break
			}
		}
	}

	raw, err := json.Marshal(record)
	if err != nil {
		return refusal("record-encode", path, 0, err.Error())
	}
	file, err := openAppend(path)
	if err != nil {
		return refusal("record-append", path, 0, err.Error())
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return refusal("record-append", path, 0, err.Error())
	}
	before := info.Size()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Truncate(before)
		return refusal("record-append", path, 0, err.Error())
	}
	if err := file.Sync(); err != nil {
		_ = file.Truncate(before)
		_ = file.Sync()
		return refusal("record-sync", path, 0, err.Error())
	}
	return nil
}

func validatePath(path string, family Family) error {
	want := ""
	switch family {
	case FamilyHistory:
		want = "history.jsonl"
	case FamilyEvidence:
		want = "evidence.jsonl"
	default:
		return fmt.Errorf("unknown family %q", family)
	}
	if filepath.Base(path) != want {
		return fmt.Errorf("family requires %s", want)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("record parent is not a directory")
	}
	return nil
}
