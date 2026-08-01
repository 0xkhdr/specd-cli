package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

type creationTransaction struct {
	Change string        `json:"change"`
	Record record.Record `json:"record"`
}

func transactionStage(owner *corepath.Owner, change string) string {
	return filepath.Join(owner.Managed(), ".new-"+change)
}

func transactionPath(owner *corepath.Owner, change string) string {
	return filepath.Join(owner.Managed(), ".new-"+change+".json")
}

func writeTransaction(owner *corepath.Owner, transaction creationTransaction) error {
	raw, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	return persist.AtomicReplace(transactionPath(owner, transaction.Change), append(raw, '\n'), nil)
}

func recoverTransactions(owner *corepath.Owner) error {
	if err := owner.CheckWriteTarget(owner.Managed()); err != nil {
		return err
	}
	return lock.With(owner.RootLock(), func() error { return recoverTransactionsLocked(owner) })
}

func recoverTransactionsLocked(owner *corepath.Owner) error {
	entries, err := os.ReadDir(owner.Managed())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stages []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".new-") {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			if entry.IsDir() {
				stages = append(stages, entry.Name())
			}
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("recover creation transaction: unsafe journal file; next: repair corrupt data outside automated mutation")
		}
		journalPath := filepath.Join(owner.Managed(), entry.Name())
		raw, err := readNoFollow(journalPath)
		if err != nil {
			return err
		}
		var transaction creationTransaction
		if err := decodeTransaction(raw, &transaction); err != nil {
			return fmt.Errorf("recover creation transaction: %w; next: repair corrupt data outside automated mutation", err)
		}
		if err := transaction.Record.Validate(); err != nil ||
			transaction.Record.Change != transaction.Change ||
			entry.Name() != ".new-"+transaction.Change+".json" {
			return fmt.Errorf("recover creation transaction: invalid transaction; next: repair corrupt data outside automated mutation")
		}
		if err := finishTransaction(owner, transaction); err != nil {
			return err
		}
	}
	// A staging directory left without a journal was never committed, so it is
	// unpublishable by definition and removing it makes the retry legal.
	for _, name := range stages {
		if _, err := os.Lstat(filepath.Join(owner.Managed(), name+".json")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("recover creation transaction: %w; next: repair corrupt data outside automated mutation", err)
		}
		if err := os.RemoveAll(filepath.Join(owner.Managed(), name)); err != nil {
			return fmt.Errorf("recover creation transaction: %w; next: repair corrupt data outside automated mutation", err)
		}
		if err := syncDirectory(owner.Managed()); err != nil {
			return err
		}
	}
	return nil
}

func finishTransaction(owner *corepath.Owner, transaction creationTransaction) error {
	stage := transactionStage(owner, transaction.Change)
	active, err := owner.Change(transaction.Change)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(active); errors.Is(err, os.ErrNotExist) {
		info, stageErr := os.Lstat(stage)
		if stageErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("publish change: unsafe staging directory; next: repair corrupt data outside automated mutation")
		}
		if err := os.Rename(stage, active); err != nil {
			return fmt.Errorf("publish change: %w", err)
		}
		if err := syncDirectory(owner.Managed()); err != nil {
			_ = os.Rename(active, stage)
			_ = syncDirectory(owner.Managed())
			return fmt.Errorf("sync published change: %w", err)
		}
	} else if err != nil {
		return err
	}
	if err := owner.CheckWriteTarget(active); err != nil {
		return err
	}
	info, err := os.Lstat(active)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("validate published change: active path is not a safe directory; next: repair corrupt data outside automated mutation")
	}
	raw, err := readNoFollow(filepath.Join(active, "state.json"))
	if err != nil {
		return fmt.Errorf("validate published change: %w", err)
	}
	current, err := state.Decode(raw, transaction.Change)
	if err != nil || current.CreatedBy != transaction.Record.ID || current.Revision != 1 {
		return fmt.Errorf("validate published change: state does not match creation transaction; next: repair corrupt data outside automated mutation")
	}
	if err := record.Append(owner.History(), record.FamilyHistory, transaction.Record); err != nil {
		if !isDuplicateRecord(err) {
			_ = os.Rename(active, stage)
			_ = syncDirectory(owner.Managed())
			return err
		}
	}
	_ = removeDurable(transactionPath(owner, transaction.Change))
	return nil
}

func isDuplicateRecord(err error) bool {
	return strings.Contains(err.Error(), "record-duplicate")
}

func syncDirectory(path string) error {
	return persist.SyncDir(path)
}

func decodeTransaction(raw []byte, transaction *creationTransaction) error {
	if err := rejectDuplicateKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(transaction); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing transaction data")
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid transaction object key")
				}
				if _, found := seen[key]; found {
					return fmt.Errorf("duplicate transaction key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing transaction data")
	}
	return nil
}
