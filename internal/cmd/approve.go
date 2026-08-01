package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
)

type ApproveOptions struct {
	Approver, Reason string
	// Input and Output carry the confirmation prompt. Both default to the
	// process's own streams; they exist so tests need no pseudo-terminal.
	// Interactivity is derived from Input, never supplied by the caller.
	Input  io.Reader
	Output io.Writer
}

// gitEmail is the single owner of the configured-identity probe. An
// unconfigured or unavailable git resolves to the empty string; identity
// validity is decided by core.ResolveApprovalIdentity, never here.
func gitEmail(root string) string {
	raw, err := exec.Command("git", "-C", root, "config", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func Approve(root, change string, options ApproveOptions) (core.ApprovalRecord, error) {
	return approveWithSources(root, change, options, gitEmail(root), os.Getenv("SPECD_APPROVER"))
}

func approveWithSources(root, change string, options ApproveOptions, gitEmail, environment string) (core.ApprovalRecord, error) {
	input, output := options.Input, options.Output
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stderr
	}
	interactive, confirmed := terminalInput(input), false
	if interactive {
		fmt.Fprintf(output, "approve %s in this terminal? [y/N]: ", change)
		line, _ := bufio.NewReader(input).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			confirmed = true
		}
	}
	return core.Approve(root, change, core.ApproveIntent{
		GitEmail: gitEmail, EnvironmentApprover: environment,
		ClaimedApprover: options.Approver, Reason: options.Reason,
		Interactive: interactive, Confirmed: confirmed,
		Route: core.ApprovalRouteHumanTerminal,
	})
}

// ponytail: ModeCharDevice also matches /dev/null, so `approve < /dev/null`
// prompts and then fails closed on EOF; upgrade to a real isatty syscall only
// if that refusal ever becomes a problem.
//
// terminalInput reports whether the confirmation stream is a terminal. A reader
// that is not a file stands in for a terminal so the prompt stays testable.
func terminalInput(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
