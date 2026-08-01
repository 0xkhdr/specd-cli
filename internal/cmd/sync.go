package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// SyncOptions mirrors ApproveOptions: sync is the second human gate, so it
// resolves identity from the same trusted sources and prompts the same way.
// Now is injected so the archive target a review shows is deterministic.
type SyncOptions struct {
	Approver, Reason string
	Input            io.Reader
	Output           io.Writer
	Now              time.Time
}

func Sync(root, change string, options SyncOptions) (core.SyncResult, error) {
	return syncWithSources(root, change, options, gitEmail(root), os.Getenv("SPECD_APPROVER"))
}

func syncWithSources(root, change string, options SyncOptions, gitEmail, environment string) (core.SyncResult, error) {
	input, output := options.Input, options.Output
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stderr
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	interactive, confirmed := terminalInput(input), false
	if interactive {
		fmt.Fprintf(output, "sync %s into accepted specs in this terminal? [y/N]: ", change)
		line, _ := bufio.NewReader(input).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			confirmed = true
		}
	}
	return core.Sync(root, change, core.SyncOptions{
		GitEmail: gitEmail, EnvironmentApprover: environment,
		ClaimedApprover: options.Approver, Reason: options.Reason,
		Interactive: interactive, Confirmed: confirmed,
		Route: core.ApprovalRouteHumanTerminal, Now: now,
	})
}
