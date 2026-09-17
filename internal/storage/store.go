package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// DevsysDirName is the per-project state directory (方案 §14.3).
const DevsysDirName = ".devsys"

// Names inside the project state directory. local/ holds locks and transaction
// recovery material and is never committed (方案 §14.2, §15.2).
const (
	localDirName    = "local"
	txnDirName      = "txn"
	lockFileName    = "lock"
	journalFileName = "journal.yaml"
	commitFileName  = "commit.yaml"
	payloadDirName  = "payload"
)

// DefaultLockTimeout bounds how long a blocked Read/Write waits before
// returning ErrLockTimeout.
const DefaultLockTimeout = 5 * time.Second

// Options tunes a Store; zero values are valid.
type Options struct {
	// LockTimeout is how long Read/Write wait for the project lock.
	LockTimeout time.Duration
	// Now supplies the current time; tests override it.
	Now func() time.Time
}

// Store gives coordinated access to the managed state under <root>/.devsys.
type Store struct {
	devsys   string
	local    string
	txnDir   string
	lockPath string
	opts     Options
}

// Open binds a project root (the directory that contains .devsys) to the
// storage primitives. The .devsys directory must already exist (devsys init);
// nothing is created here.
func Open(root string, opts Options) (*Store, error) {
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = DefaultLockTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	devsys := filepath.Join(root, DevsysDirName)
	st, err := os.Stat(devsys)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %s", ErrNotInitialized, devsys)
	case err != nil:
		return nil, fmt.Errorf("stat %s: %w", devsys, err)
	case !st.IsDir():
		return nil, fmt.Errorf("%w: %s is not a directory", ErrNotInitialized, devsys)
	}
	local := filepath.Join(devsys, localDirName)
	return &Store{
		devsys:   devsys,
		local:    local,
		txnDir:   filepath.Join(local, txnDirName),
		lockPath: filepath.Join(local, lockFileName),
		opts:     opts,
	}, nil
}

// DevsysDir returns the managed state directory.
func (s *Store) DevsysDir() string { return s.devsys }

// LocalDir returns the local-only directory that holds locks and transaction
// material.
func (s *Store) LocalDir() string { return s.local }

// resolve maps a managed path ("workitems/x.yaml", slash-separated, relative to
// .devsys) to an absolute path, rejecting anything that would leave the managed
// tree or collide with storage-internal material.
func (s *Store) resolve(rel string) (string, error) {
	if rel == "" || path.IsAbs(rel) || strings.ContainsAny(rel, `\:`) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	if clean == "local/"+lockFileName || clean == "local/"+txnDirName || strings.HasPrefix(clean, "local/"+txnDirName+"/") {
		return "", fmt.Errorf("%w: %q is reserved for storage material", ErrUnsafePath, rel)
	}
	abs := filepath.Join(s.devsys, filepath.FromSlash(clean))
	if !strings.HasPrefix(abs, s.devsys+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	return abs, nil
}

// Reader exposes consistent reads: it runs under the shared project lock, and
// Read/Write recover interrupted transactions first, so callers never observe a
// half-applied multi-file change (方案 §15.2 有效状态读取须与写入、恢复协调).
type Reader struct{ s *Store }

// Read returns the content of a managed file; a missing file yields exists
// false rather than an error.
func (r *Reader) Read(rel string) ([]byte, bool, error) {
	abs, err := r.s.resolve(rel)
	if err != nil {
		return nil, false, err
	}
	data, exists, err := readFileMaybe(abs)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", rel, err)
	}
	return data, exists, nil
}

// Expect reads rel and returns the version guard that makes a later Put fail if
// anyone else changed the file in between (optimistic concurrency).
func (r *Reader) Expect(rel string) (Expect, error) {
	data, exists, err := r.Read(rel)
	if err != nil {
		return Expect{}, err
	}
	if !exists {
		return ExpectAbsent(), nil
	}
	return ExpectHash(HashBytes(data)), nil
}

// Read runs fn under the shared project lock. When an interrupted transaction
// needs recovery it upgrades to the exclusive lock, recovers deterministically
// and retries; a failed recovery is reported instead of reading partial state.
func (s *Store) Read(ctx context.Context, fn func(r *Reader) error) error {
	const maxAttempts = 3
	for range maxAttempts {
		lock, err := acquireLock(ctx, s.lockPath, lockShared, s.opts.LockTimeout, s.opts.Now)
		if err != nil {
			return err
		}
		names, err := s.pendingTxns()
		if err != nil {
			lock.release()
			return err
		}
		if len(names) == 0 {
			err := fn(&Reader{s: s})
			lock.release()
			return err
		}
		lock.release()
		if _, err := s.Recover(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: pending transactions keep appearing; run recovery and retry", ErrRecoveryFailed)
}

// Write runs fn under the exclusive project lock and applies the collected
// changes as one recoverable transaction. fn errors abort the whole
// transaction: nothing reaches the managed files. An empty transaction is a
// no-op.
func (s *Store) Write(ctx context.Context, fn func(tx *Tx) error) error {
	lock, err := acquireLock(ctx, s.lockPath, lockExclusive, s.opts.LockTimeout, s.opts.Now)
	if err != nil {
		return err
	}
	defer lock.release()

	if _, err := s.recoverLocked(); err != nil {
		return err
	}
	tx := &Tx{s: s, seen: make(map[string]bool)}
	if err := fn(tx); err != nil {
		return err
	}
	if len(tx.ops) == 0 {
		return nil
	}
	return s.commitLocked(tx)
}
