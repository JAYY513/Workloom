package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

//
//   - When both preconditions are met, Inspect opens the existing lock
//     file with O_RDONLY (no O_CREATE), takes the shared OS lock, runs
//     the caller's fn under a Reader, and releases the lock. The shared
//     lock blocks concurrent writers; readers do not block each other.
//

// ErrLockUnavailable reports that the project lock file does not exist,
// so a read-only command cannot take the shared lock without creating
// state. Callers should report "inspection unavailable" rather than
// fabricating a "no activity" claim.
var ErrLockUnavailable = errors.New("storage: project lock unavailable (no .devsys/local/lock)")

// ErrPendingTxn reports that Inspect refuses to run while pending
// transactions exist on disk. Callers must surface the pending IDs and
// refuse to render business facts (方案 §15.4 待恢复事务不作为有效业务状态返回).
var ErrPendingTxn = errors.New("storage: pending transactions block inspection")

// PendingTxnError wraps the pending transaction IDs alongside ErrPendingTxn
// so callers can render them without re-scanning the directory.
type PendingTxnError struct {
	IDs []string
}

func (e *PendingTxnError) Error() string {
	return fmt.Sprintf("%s: %v", ErrPendingTxn.Error(), e.IDs)
}

func (e *PendingTxnError) Unwrap() error { return ErrPendingTxn }

// Inspect runs fn under the shared project lock without auto-recovery and
// without creating the lock file. See package doc for the contract.
func (s *Store) Inspect(ctx context.Context, fn func(r *Reader) error) error {
	// Step 1: refuse to run while pending transactions exist. Doctor's
	// caller wants a stable snapshot; the existence of an unfinished
	// journal entry means we cannot give one. We list pending txns
	// without taking the lock (pendingTxns uses os.ReadDir only).
	names, err := s.pendingTxns()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return &PendingTxnError{IDs: names}
	}
	// Step 2: open the existing lock file without O_CREATE. When no
	// writer ever ran there is no lock file; reading it with O_CREATE
	// would write state, so surface ErrLockUnavailable and let the
	// caller decide (doctor reports "inspection unavailable").
	if _, statErr := os.Stat(s.lockPath); errors.Is(statErr, fs.ErrNotExist) {
		return ErrLockUnavailable
	}
	f, err := os.OpenFile(s.lockPath, os.O_RDONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open lock file %s: %w", s.lockPath, err)
	}
	// Take the shared OS lock. lockHandle is the platform-specific
	// primitive (flock on POSIX, LockFileEx on Windows). We retry
	// briefly with the configured LockTimeout so a brief writer does
	// not cause Doctor to flap, but we do NOT block forever.
	if err := acquireSharedLock(ctx, f, s.opts.LockTimeout, s.opts.Now); err != nil {
		f.Close()
		return err
	}
	defer func() {
		_ = releaseSharedLock(f)
		_ = f.Close()
	}()
	return fn(&Reader{s: s})
}

// InspectUnlocked runs fn without the project lock. It is the conservative
// fallback for read-only diagnostics when no lock file exists yet (no writer
// ever ran): every file read is still a single atomic-replace target, so
// readers observe whole files. A concurrent first write can still race;
// callers must treat the snapshot as advisory and re-check critical evidence
// under a real transaction (方案 §15.4: 只读诊断不得误判).
func (s *Store) InspectUnlocked(ctx context.Context, fn func(r *Reader) error) error {
	names, err := s.pendingTxns()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return &PendingTxnError{IDs: names}
	}
	return fn(&Reader{s: s})
}

// acquireSharedLock takes the shared OS lock on f, retrying until timeout
// or ctx cancellation. It does NOT create the lock file.
func acquireSharedLock(ctx context.Context, f *os.File, timeout time.Duration, now func() time.Time) error {
	deadline := now().Add(timeout)
	delay := 5 * time.Millisecond
	for {
		if err := lockHandle(f, false); err == nil {
			return nil
		} else if !lockContended(err) {
			return fmt.Errorf("acquire shared lock: %w", err)
		}
		if !now().Before(deadline) {
			holder := holderInfoFromFile(f)
			if holder != "" {
				return fmt.Errorf("%w: %s held by %s", ErrLockTimeout, f.Name(), holder)
			}
			return fmt.Errorf("%w: %s", ErrLockTimeout, f.Name())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 40*time.Millisecond {
			delay += 5 * time.Millisecond
		}
	}
}

// releaseSharedLock is a thin wrapper that calls unlockHandle.
func releaseSharedLock(f *os.File) error { return unlockHandle(f) }

// holderInfoFromFile reads the best-effort holder description left in the
// lock file by an exclusive holder. Used by Inspect's timeout message;
// the read is best-effort and never blocks the caller past timeout.
func holderInfoFromFile(f *os.File) string {
	if _, err := f.Seek(0, 0); err != nil {
		return ""
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}
	return strings.Join(strings.Fields(string(buf[:n])), " ")
}
