//go:build windows

package verify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	processSetQuota       = 0x0100
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createJobObject          = kernel32.NewProc("CreateJobObjectW")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	terminateJobObject       = kernel32.NewProc("TerminateJobObject")
)

// jobs holds the job object bounding each running verification. Windows has no
// process group to signal, and taskkill cannot be trusted to report the answer:
// it exits non-zero when a descendant merely finished on its own, which is
// indistinguishable from a descendant it failed to kill. A job object is the
// kernel's own answer — every descendant inherits it, and terminating the job
// terminates all of them as one unit, so "the tree is gone" is observed rather
// than parsed out of localized console text.
var jobs sync.Map // *exec.Cmd -> syscall.Handle

func configureProcess(process *exec.Cmd) {
	process.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func shellCommand(command string) (string, []string) {
	return "cmd.exe", []string{"/d", "/s", "/c", command}
}

// afterStart puts the started process into a fresh job object. Assignment
// cannot happen before the process exists, which leaves the interval between
// CreateProcess and this call: a descendant spawned inside it would escape the
// job. That window closes before the shell has read its command line, and the
// alternative — resuming a suspended process — needs a thread handle the
// standard library does not expose.
func afterStart(process *exec.Cmd) error {
	if process.Process == nil {
		return errors.New("bound the verification process tree before it started")
	}
	handle, _, callErr := createJobObject.Call(0, 0)
	if handle == 0 {
		return fmt.Errorf("create verification job object: %w", callErr)
	}
	job := syscall.Handle(handle)

	// The process is still held open by os.Process, so its identifier cannot
	// have been reused by the time this opens a second handle to it.
	target, err := syscall.OpenProcess(processSetQuota|syscall.PROCESS_TERMINATE,
		false, uint32(process.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(job)
		return fmt.Errorf("open verification process: %w", err)
	}
	defer syscall.CloseHandle(target)

	if assigned, _, callErr := assignProcessToJobObject.Call(uintptr(job), uintptr(target)); assigned == 0 {
		_ = syscall.CloseHandle(job)
		return fmt.Errorf("assign verification process to job object: %w", callErr)
	}
	jobs.Store(process, job)
	return nil
}

// releaseProcess closes the job handle once the process has been waited on.
// The job is not marked kill-on-close: a tree that outlived its own parent is
// terminated by an explicit timeout or cancellation, exactly as the Unix build
// only signals its process group on cancellation.
func releaseProcess(process *exec.Cmd) {
	if job, found := jobs.LoadAndDelete(process); found {
		_ = syscall.CloseHandle(job.(syscall.Handle))
	}
}

func terminateProcessTree(process *exec.Cmd) error {
	if process.Process == nil {
		return os.ErrProcessDone
	}
	job, found := jobs.Load(process)
	if !found {
		return errors.New("verification process tree is not bound to a job object")
	}
	if terminated, _, err := terminateJobObject.Call(uintptr(job.(syscall.Handle)), 1); terminated == 0 {
		return fmt.Errorf("terminate verification job object: %w", err)
	}
	return nil
}
