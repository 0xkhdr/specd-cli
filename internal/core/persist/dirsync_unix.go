//go:build !windows

package persist

import "os"

// SyncDir flushes a directory entry so a rename that has already returned
// survives a crash. It is the one owner of that call: a managed write is
// durable only when the entry naming its new bytes is on disk too.
func SyncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
