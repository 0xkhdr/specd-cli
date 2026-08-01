package cmd

import (
	"fmt"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
)

func Check(root, change string) (core.CheckResult, error) {
	return core.RunCheck(root, change, core.DefaultPolicyDigest())
}

func RenderCheck(result core.CheckResult) string {
	if len(result.Findings) == 0 {
		return fmt.Sprintf("check passed for %s revision %d; root %s\n",
			result.Change, result.StateRevision, result.Root)
	}
	var output strings.Builder
	for _, finding := range result.Findings {
		location := finding.Location.Path
		if finding.Location.Line > 0 {
			location += fmt.Sprintf(":%d", finding.Location.Line)
		}
		if location != "" {
			location += ": "
		}
		fmt.Fprintf(&output, "%s %s: %s%s; repair: %s\n",
			finding.Severity, finding.Gate, location, finding.Problem, finding.Repair)
	}
	status := "failed"
	if result.Success {
		status = "passed with warnings"
	}
	fmt.Fprintf(&output, "check %s for %s revision %d; root %s\n",
		status, result.Change, result.StateRevision, result.Root)
	return output.String()
}
