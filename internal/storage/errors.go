// Package storage implements the text state primitives required by 方案 §15.2:
// atomic single-file replacement, JSONL appends with torn-tail detection,
// stable-key YAML/JSON serialization, a project-level short write lock,
// optimistic version checks and a recoverable multi-file transaction log.
//
// Facts this package does *not* claim: file-system crash consistency under
// power loss. It provides single-file atomicity (temp file + rename), process
// interruption recovery (journal replay), and best-effort fsync. Power-loss
// durability is out of scope until separately verified.
package storage

import "errors"

var (
	// ErrConflict reports that a staged update expected an older version of a
	// managed file than the one currently on disk (方案 §15.2 乐观并发控制).
	ErrConflict = errors.New("storage: version conflict")

	// ErrLockTimeout reports that the project lock stayed busy for the whole
	// LockTimeout window.
	ErrLockTimeout = errors.New("storage: project lock timeout")

	// ErrRecoveryFailed reports that an interrupted transaction could not be
	// recovered deterministically. Valid-state reads and writes are refused
	// until it is resolved; diagnostics remain available.
	ErrRecoveryFailed = errors.New("storage: transaction recovery failed")

	// ErrIncompleteTail reports a JSONL file whose last line has no line
	// terminator (a torn append).
	ErrIncompleteTail = errors.New("storage: jsonl incomplete tail")

	// ErrUnsafePath reports a managed path that would leave the .devsys tree
	// or collide with storage-internal material.
	ErrUnsafePath = errors.New("storage: unsafe managed path")

	// ErrSchemaVersion reports a missing or unsupported schema_version.
	ErrSchemaVersion = errors.New("storage: schema_version rejected")

	// ErrNotInitialized reports a project root without a .devsys directory.
	ErrNotInitialized = errors.New("storage: project not initialized")
)
