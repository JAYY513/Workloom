package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type lockMode int

const (
	lockShared lockMode = iota
	lockExclusive
)

// projectLock is an OS-level advisory lock on .devsys/local/lock. OS locks are
// released automatically when the owning process dies, so an interrupted devsys
// never leaves the project permanently locked (方案 §15.2 项目级短时写锁).
type projectLock struct {
	file *os.File
	mode lockMode
}

// acquireLock opens the lock file and takes the requested lock, waiting up to
// timeout and aborting when ctx is cancelled.
func acquireLock(ctx context.Context, path string, mode lockMode, timeout time.Duration, now func() time.Time) (*projectLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	exclusive := mode == lockExclusive
	deadline := now().Add(timeout)
	delay := 5 * time.Millisecond
	for {
		err := lockHandle(f, exclusive)
		if err == nil {
			l := &projectLock{file: f, mode: mode}
			l.describe(now)
			return l, nil
		}
		if !lockContended(err) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if !now().Before(deadline) {
			holder := holderInfo(path)
			f.Close()
			if holder != "" {
				return nil, fmt.Errorf("%w: %s held by %s", ErrLockTimeout, path, holder)
			}
			return nil, fmt.Errorf("%w: %s is held by another devsys process", ErrLockTimeout, path)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		if delay < 40*time.Millisecond {
			delay += 5 * time.Millisecond
		}
	}
}

// describe records who holds the lock so blocked writers and Diagnose can name
// the holder. Only exclusive holders write: shared holders must not rewrite the
// file under each other.
func (l *projectLock) describe(now func() time.Time) {
	if l.mode != lockExclusive {
		return
	}
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return
	}
	if err := l.file.Truncate(0); err != nil {
		return
	}
	_, _ = fmt.Fprintf(l.file, "pid: %d\nmode: exclusive\nsince: %s\n",
		os.Getpid(), now().UTC().Format(time.RFC3339Nano))
	_ = l.file.Sync()
}

func (l *projectLock) release() error {
	err := unlockHandle(l.file)
	if cerr := l.file.Close(); err == nil {
		err = cerr
	}
	return err
}

// holderInfo reads the best-effort holder description left by the last
// exclusive holder. It is informational only.
func holderInfo(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	return strings.Join(strings.Fields(string(data)), " ")
}
