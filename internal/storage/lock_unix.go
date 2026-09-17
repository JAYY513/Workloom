//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

// POSIX locking uses flock(2): LOCK_SH for readers and LOCK_EX for writers,
// both non-blocking so the caller controls the wait policy.
func lockHandle(f *os.File, exclusive bool) error {
	how := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		how = syscall.LOCK_EX | syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

func unlockHandle(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func lockContended(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

// transientRenameError: POSIX rename replaces the destination atomically even
// while another handle has it open, so no retry is needed.
func transientRenameError(error) bool { return false }
