//go:build !windows

package verify

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(process *exec.Cmd) {
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func shellCommand(command string) (string, []string) {
	return "/bin/sh", []string{"-c", command}
}

// afterStart has nothing to do here: Setpgid already put the process and every
// descendant it spawns in one signalable group before it ran.
func afterStart(*exec.Cmd) error { return nil }

func releaseProcess(*exec.Cmd) {}

func terminateProcessTree(process *exec.Cmd) error {
	if process.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
