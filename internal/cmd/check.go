package cmd

import "github.com/0xkhdr/specd-cli/internal/core"

// Check runs the planning gates under the default policy. Findings reach both
// surfaces as envelope diagnostics; this package renders nothing of its own.
func Check(root, change string) (core.CheckResult, error) {
	return core.RunCheck(root, change, core.DefaultPolicyDigest())
}
