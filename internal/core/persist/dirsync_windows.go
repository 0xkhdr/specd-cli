//go:build windows

package persist

// SyncDir does nothing on Windows, because Windows exposes no directory flush
// to call: FlushFileBuffers refuses a directory handle with
// ERROR_ACCESS_DENIED, which is what made every managed write on this platform
// fail as an I/O refusal. The rename's own durability rests on NTFS metadata
// journaling instead, which is a weaker guarantee than an fsync'd directory
// entry and is recorded as such in release/release-decision.md.
func SyncDir(string) error { return nil }
