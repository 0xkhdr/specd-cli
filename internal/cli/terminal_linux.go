package cli

import "syscall"

// ioctlReadTermios is the Linux request that reads a terminal's attributes.
const ioctlReadTermios = syscall.TCGETS
