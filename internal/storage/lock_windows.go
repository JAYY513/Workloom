//go:build windows

package storage

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Windows locking goes through LockFileEx. The standard library's syscall
// package does not export it (verified: undefined: syscall.LockFileEx), so it
// is resolved dynamically from kernel32.
//
// The locked range is a single byte far beyond any content (offset 1 GiB).
// Windows byte-range locks block reads and writes of the locked range from
// other handles, so locking offset 0 would make the best-effort holder
// description unreadable while a writer runs.
const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
	errorLockViolation      = 33
	lockByteOffset          = 1 << 30
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockHandle(f *os.File, exclusive bool) error {
	flags := uint32(lockfileFailImmediately)
	if exclusive {
		flags |= lockfileExclusiveLock
	}
	ol := syscall.Overlapped{Offset: lockByteOffset}
	r1, _, err := procLockFileEx.Call(uintptr(f.Fd()), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r1 == 0 {
		return err
	}
	return nil
}

func unlockHandle(f *os.File) error {
	ol := syscall.Overlapped{Offset: lockByteOffset}
	r1, _, err := procUnlockFileEx.Call(uintptr(f.Fd()), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r1 == 0 {
		return err
	}
	return nil
}

// lockContended reports whether err means another process holds the lock.
func lockContended(err error) bool {
	return errors.Is(err, syscall.Errno(errorLockViolation))
}

// transientRenameError reports renames worth retrying. On Windows renaming over
// a file another handle holds open fails with ERROR_ACCESS_DENIED because Go's
// os.Open does not request FILE_SHARE_DELETE (verified).
func transientRenameError(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.Errno(32)) // 32 = ERROR_SHARING_VIOLATION
}
