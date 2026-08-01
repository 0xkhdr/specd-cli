//go:build windows

package cli

import (
	"os"
	"syscall"
)

// isTerminal reports whether the file is an actual Windows console, by asking
// the console host for the handle's mode. GetConsoleMode is the platform's
// equivalent of the termios ioctl the Unix build uses: only a console handle
// answers it. A pipe, a regular file, and NUL all fail it with
// ERROR_INVALID_HANDLE, so the derivation stays one-directional here too —
// anything that is not proven to be a console derives the agent route.
func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode) == nil
}
