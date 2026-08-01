//go:build windows

package verify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

var runTaskkill = func(pid int) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
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
