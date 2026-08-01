//go:build windows

package verify

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRunWindowsUnboundTreeIsExplicit pins the fail-closed half of tree
// termination: a process that was never bound to a job object cannot have its
// descendants terminated, and that must be reported as a failure rather than
// mistaken for a tree that was already gone.
func TestRunWindowsUnboundTreeIsExplicit(t *testing.T) {
	process := exec.Command("cmd.exe", "/d", "/c", "ping -n 30 127.0.0.1 >nul")
	configureProcess(process)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	}()

	err := terminateProcessTree(process)
	if err == nil || !strings.Contains(err.Error(), "not bound to a job object") {
		t.Fatalf("termination error = %v", err)
	}
}

// TestRunWindowsJobTerminatesTheTree is the other half: a bound tree dies as
// one unit, including a descendant the root process spawned.
func TestRunWindowsJobTerminatesTheTree(t *testing.T) {
	process := exec.Command("cmd.exe", "/d", "/c", "start /b ping -n 30 127.0.0.1 >nul & ping -n 30 127.0.0.1 >nul")
	configureProcess(process)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	if err := afterStart(process); err != nil {
		t.Fatalf("bind the process tree: %v", err)
	}
	defer releaseProcess(process)

	if err := terminateProcessTree(process); err != nil {
		t.Fatalf("terminate the process tree: %v", err)
	}
	if err := process.Wait(); err == nil {
		t.Fatal("terminated process reported success")
	}
}
