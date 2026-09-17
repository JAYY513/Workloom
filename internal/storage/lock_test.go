package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockModesWithinOneProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	ctx := context.Background()
	now := time.Now

	ex, err := acquireLock(ctx, path, lockExclusive, time.Second, now)
	if err != nil {
		t.Fatalf("exclusive: %v", err)
	}
	if _, err := acquireLock(ctx, path, lockExclusive, 50*time.Millisecond, now); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("second exclusive error = %v, want ErrLockTimeout", err)
	}
	if _, err := acquireLock(ctx, path, lockShared, 50*time.Millisecond, now); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("shared while exclusive error = %v, want ErrLockTimeout", err)
	}
	if err := ex.release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	sh1, err := acquireLock(ctx, path, lockShared, time.Second, now)
	if err != nil {
		t.Fatalf("first shared: %v", err)
	}
	sh2, err := acquireLock(ctx, path, lockShared, time.Second, now)
	if err != nil {
		t.Fatalf("second shared: %v", err)
	}
	if _, err := acquireLock(ctx, path, lockExclusive, 50*time.Millisecond, now); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("exclusive while shared error = %v, want ErrLockTimeout", err)
	}
	sh1.release()
	sh2.release()

	ex2, err := acquireLock(ctx, path, lockExclusive, time.Second, now)
	if err != nil {
		t.Fatalf("exclusive after release: %v", err)
	}
	if err := ex2.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestLockTimeoutNamesTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	ctx := context.Background()
	holder, err := acquireLock(ctx, path, lockExclusive, time.Second, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.release()

	_, err = acquireLock(ctx, path, lockExclusive, 50*time.Millisecond, time.Now)
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("error = %v, want ErrLockTimeout", err)
	}
	if !strings.Contains(err.Error(), "pid:") {
		t.Errorf("error does not name the holder: %v", err)
	}
}

func TestLockTimeoutAcrossProcesses(t *testing.T) {
	_, root := newTestStore(t)
	start := filepath.Join(t.TempDir(), "holding")
	cmd := helperCmd(t, "holder", root, map[string]string{
		envStart:   start,
		envHoldMS:  "1500",
		envErrFile: filepath.Join(t.TempDir(), "err.txt"),
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, start)

	blocked, err := Open(root, Options{LockTimeout: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	err = blocked.Write(context.Background(), func(tx *Tx) error {
		return tx.Put(stateRel, []byte("blocked\n"), ExpectAny())
	})
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("write error = %v, want ErrLockTimeout", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("holder process: %v", err)
	}
	if err := blocked.Write(context.Background(), func(tx *Tx) error {
		return tx.Put(stateRel, []byte("after\n"), ExpectAny())
	}); err != nil {
		t.Fatalf("write after the holder exited: %v", err)
	}
}
