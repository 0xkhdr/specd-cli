//go:build linux || darwin

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal reports whether the file is an actual controlling terminal, by
// asking the kernel for its line discipline. Only a terminal answers the
// termios ioctl; a pipe, a regular file, and /dev/null all refuse it.
//
// A character-device check is not this test. /dev/null is a character device,
// so every agent shell that inherits it would otherwise derive the human route
// and reach the one gate no agent may pass.
func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(),
		ioctlReadTermios, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
