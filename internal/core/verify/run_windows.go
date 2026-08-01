//go:build windows

package verify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const createNewProcessGroup = 0x00000200

// runTaskkill terminates the process tree and reports why it could not. The
// command's own output is the only account of a partial kill, so a refusal that
// drops it leaves an operator with an exit status and no reason.
var runTaskkill = func(pid int) error {
	output, err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).CombinedOutput()
	if err == nil {
		return nil
	}
	if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
		return fmt.Errorf("%w: %s", err, trimmed)
	}
	return err
}

func configureProcess(process *exec.Cmd) {
	process.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func shellCommand(command string) (string, []string) {
	return "cmd.exe", []string{"/d", "/s", "/c", command}
}

func terminateProcessTree(process *exec.Cmd) error {
	if process.Process == nil {
		return os.ErrProcessDone
	}
	err := runTaskkill(process.Process.Pid)
	if err == nil {
		return nil
	}
	// Kill the root as cleanup, but preserve taskkill failure: descendant
	// termination was not verified and must never be reported as successful.
	killErr := process.Process.Kill()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return fmt.Errorf("taskkill: %v; root kill: %w", err, killErr)
	}
	return fmt.Errorf("taskkill did not verify descendant termination: %w", err)
}
