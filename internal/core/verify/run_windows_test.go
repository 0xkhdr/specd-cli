//go:build windows

package verify

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRunWindowsTaskkillFailureIsExplicit(t *testing.T) {
	process := exec.Command("cmd.exe", "/d", "/c", "ping -n 30 127.0.0.1 >nul")
	configureProcess(process)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	original := runTaskkill
	runTaskkill = func(int) error { return errors.New("taskkill unavailable") }
	t.Cleanup(func() { runTaskkill = original })
	err := terminateProcessTree(process)
	_ = process.Wait()
	if err == nil || !strings.Contains(err.Error(), "did not verify descendant termination") {
		t.Fatalf("termination error = %v", err)
	}
}
