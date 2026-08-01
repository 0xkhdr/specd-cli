package persist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrDirectorySyncUnsupported = errors.New("directory sync unsupported")

// Hook is called at stable failure-injection points. It is nil in production.
type Hook func(step string) error

// AtomicReplace durably replaces path with data. The caller owns validation.
func AtomicReplace(path string, data []byte, hook Hook) error {
	dir := filepath.Dir(path)
	if err := call(hook, "before-create"); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tmp := f.Name()
	renamed := false
	defer func() {
		_ = f.Close()
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()

	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary state mode: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := call(hook, "after-write"); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := call(hook, "after-sync"); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := call(hook, "before-rename"); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	renamed = true

	if err := SyncDir(dir); err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return fmt.Errorf("%w: %v", ErrDirectorySyncUnsupported, err)
		}
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}

func call(h Hook, step string) error {
	if h == nil {
		return nil
	}
	if err := h(step); err != nil {
		return fmt.Errorf("%s: %w", step, err)
	}
	return nil
}
