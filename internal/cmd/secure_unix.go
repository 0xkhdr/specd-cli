//go:build !windows

package cmd

import (
	"io"
	"os"
	"syscall"
)

func readNoFollow(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	return io.ReadAll(file)
}
