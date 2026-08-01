package cli

import "syscall"

// ioctlReadTermios is the Darwin request that reads a terminal's attributes.
const ioctlReadTermios = syscall.TIOCGETA
