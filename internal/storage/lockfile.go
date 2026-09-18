package storage

import (
	"errors"
	"fmt"
	"os"
)

// ErrLocked reports that another holder has the lock.
var ErrLocked = errors.New("lock is held by another process")

// LockFile takes a non-blocking exclusive advisory lock on path, creating the
// file when needed, and returns the release function. It is the same primitive
// the store uses for project-level write locks (方案 §15.2): the OS releases it
// when the process dies, so an interrupted run never leaves the file locked.
//
// Contention is reported as ErrLocked rather than waited out: a caller that
// wants to wait can retry, and a caller that must not run twice (a long
// refresh) gets a straight answer.
func LockFile(path string) (func() error, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := lockHandle(file, true); err != nil {
		_ = file.Close()
		if lockContended(err) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() error {
		if err := unlockHandle(file); err != nil {
			_ = file.Close()
			return fmt.Errorf("unlock %s: %w", path, err)
		}
		return file.Close()
	}, nil
}
