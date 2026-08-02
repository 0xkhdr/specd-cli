package cmd

import (
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// ArchiveOptions carries the acting identity and the injected clock. Now is
// local time on purpose: the archive prefix is a local calendar date.
type ArchiveOptions struct {
	Actor string
	Now   time.Time
}

// Archive moves one reconciled change into the local archive. The actor is
// checked by core.Archive, which owns that refusal; this entry only supplies
// the clock core requires.
func Archive(root, change string, options ArchiveOptions) (core.ArchiveResult, error) {
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	return core.Archive(root, change, core.ArchiveOptions{Actor: options.Actor, Now: now})
}
