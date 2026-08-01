package cmd

import (
	"errors"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// ArchiveOptions carries the acting identity and the injected clock. Now is
// local time on purpose: the archive prefix is a local calendar date.
type ArchiveOptions struct {
	Actor string
	Now   time.Time
}

func Archive(root, change string, options ArchiveOptions) (core.ArchiveResult, error) {
	if strings.TrimSpace(options.Actor) == "" {
		return core.ArchiveResult{}, errors.New(
			"archive actor is required; next: retry through an authorized harness operation",
		)
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	return core.Archive(root, change, core.ArchiveOptions{Actor: options.Actor, Now: now})
}
