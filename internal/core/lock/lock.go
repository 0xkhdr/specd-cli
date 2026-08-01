package lock

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

var local sync.Map

// Deadline bounds how long one operation waits for a contended lock. A wedged
// holder must produce a refusal with a legal recovery action, never a hang.
var Deadline = 10 * time.Second

// retryInterval is the poll gap while waiting. Both platform locks are
// non-blocking, so waiting is a bounded poll rather than an unbounded syscall.
const retryInterval = 20 * time.Millisecond

// With serializes work across processes and goroutines. The OS releases the
// file lock if its process exits, so no unsafe stale-lock deletion is needed.
// Contention past Deadline refuses instead of blocking forever.
func With(path string, fn func() error) error {
	path = filepath.Clean(path)
	value, _ := local.LoadOrStore(path, new(sync.Mutex))
	mutex := value.(*sync.Mutex)
	if !waitFor(mutex.TryLock) {
		return busy(path)
	}
	defer mutex.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return failure.New("lock_failed", "", path, err.Error(), "retry the operation")
	}
	file, err := secureOpen(path)
	if err != nil {
		return failure.New("lock_failed", "", path, err.Error(), "retry the operation")
	}
	defer file.Close()

	var lockErr error
	held := waitFor(func() bool {
		acquired, err := platformTryLock(file)
		lockErr = err
		return acquired || err != nil
	})
	switch {
	case lockErr != nil:
		return failure.New("lock_failed", "", path, lockErr.Error(), "retry the operation")
	case !held:
		return busy(path)
	}
	defer platformUnlock(file)
	return fn()
}

// waitFor polls attempt until it succeeds or Deadline passes. It tries once
// before sleeping, so an uncontended lock costs no delay.
func waitFor(attempt func() bool) bool {
	expiry := time.Now().Add(Deadline)
	for {
		if attempt() {
			return true
		}
		if time.Now().After(expiry) {
			return false
		}
		time.Sleep(retryInterval)
	}
}

func busy(path string) error {
	return failure.New("lock_busy", "", path,
		"another specd operation held this lock for longer than "+Deadline.String(),
		"wait for the other operation to finish, then retry the operation")
}
