//go:build windows

package lock

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	fileFlagOpenReparsePoint = 0x00200000

	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002

	errorLockViolation = syscall.Errno(33)
)

// platformTryLock takes the exclusive lock without blocking. A lock already
// held by another process reports (false, nil) so the caller can bound the wait.
func platformTryLock(file *os.File) (bool, error) {
	var overlapped syscall.Overlapped
	result, _, err := lockFileEx.Call(file.Fd(), lockfileExclusiveLock|lockfileFailImmediately,
		0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result != 0 {
		return true, nil
	}
	if err == errorLockViolation {
		return false, nil
	}
	return false, err
}

func platformUnlock(file *os.File) {
	var overlapped syscall.Overlapped
	_, _, _ = unlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
}

func secureOpen(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagOpenReparsePoint, 0)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, syscall.ERROR_ACCESS_DENIED
	}
	return os.NewFile(uintptr(handle), path), nil
}
