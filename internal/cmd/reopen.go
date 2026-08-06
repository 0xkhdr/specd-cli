package cmd

import "github.com/0xkhdr/specd-cli/internal/core"

// Reopen adapts CLI values once; core owns every recovery decision.
func Reopen(root, change string, revision uint64, reason, actor string) (core.ReopenResult, error) {
	return core.Reopen(root, change, core.ReopenIntent{
		ExpectedRevision: revision, Reason: reason, Actor: actor,
	})
}
