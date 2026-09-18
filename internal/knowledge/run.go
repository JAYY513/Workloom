package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"workloom/internal/storage"
)

// RunSchemaVersion is the only checkpoint version this build reads or writes.
const RunSchemaVersion = 1

// RunLockFile is the refresh mutex: an OS-level exclusive lock, released when
// the process dies, so a killed refresh never blocks the next one.
const RunLockFile = Dir + "/run.lock"

// Checkpoint phases.
const (
	PhaseGenerating = "generating"
	PhaseFinished   = "finished"
	PhaseFailed     = "failed"
)

// Checkpoint is the refresh's progress record (.devsys/knowledge/run.json,
// 方案 §12.5). It exists to make an interrupted refresh resumable instead of
// restartable: a killed run leaves it behind, and the next run reduces its page
// list to the pages that still do not match their record.
type Checkpoint struct {
	SchemaVersion int       `json:"schema_version"`
	PID           int       `json:"pid"`
	Host          string    `json:"host,omitempty"`
	Phase         string    `json:"phase"`
	Mode          string    `json:"mode"`
	Generator     string    `json:"generator,omitempty"`
	ScopeFile     string    `json:"scope_file,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// Pages is the set this run asked the generator for; Completed is what the
	// run had confirmed when it last wrote the checkpoint.
	Pages     []string `json:"pages"`
	Completed []string `json:"completed,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// LoadCheckpoint reads the checkpoint; a missing file reports fs.ErrNotExist.
func LoadCheckpoint(root string) (*Checkpoint, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(RunFile)))
	if err != nil {
		return nil, err
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("%s: %w", RunFile, err)
	}
	if checkpoint.SchemaVersion != RunSchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d is not supported (this build reads %d)",
			RunFile, checkpoint.SchemaVersion, RunSchemaVersion)
	}
	return &checkpoint, nil
}

// WriteCheckpoint stores the checkpoint atomically.
func WriteCheckpoint(root string, checkpoint *Checkpoint) (string, error) {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode checkpoint: %w", err)
	}
	data = append(data, '\n')
	if err := storage.AtomicWrite(filepath.Join(root, filepath.FromSlash(RunFile)), data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", RunFile, err)
	}
	return RunFile, nil
}

// ClearCheckpoint removes the checkpoint after a run finished cleanly. A
// missing file is not an error: the run may have been the one that created it.
func ClearCheckpoint(root string) error {
	err := os.Remove(filepath.Join(root, filepath.FromSlash(RunFile)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", RunFile, err)
	}
	return nil
}

// LockRefresh takes the refresh mutex. A held lock means another refresh is
// running; releasing is the caller's job. The lock file's directory is created
// when needed: a project that has never refreshed has no .devsys/knowledge yet,
// and a mutex that cannot exist would make the first refresh fail.
func LockRefresh(root string) (func() error, error) {
	path := filepath.Join(root, filepath.FromSlash(RunLockFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", Dir, err)
	}
	return storage.LockFile(path)
}

// MissingCheckpoint reports whether the error means "there is no checkpoint".
func MissingCheckpoint(err error) bool {
	return err != nil && errors.Is(err, os.ErrNotExist)
}

// ResumePending reduces an interrupted run's page list to the pages that still
// need regenerating. A page whose recorded content hash already matches the
// file on disk was finished before the interruption, and the generator contract
// makes re-running it harmless but pointless (方案 §12.6: 幂等); this is what
// keeps a resumed run from rewriting work that is done.
func ResumePending(root string, pages []*Page, state *State, pending []string) ([]string, error) {
	byPath := map[string]*Page{}
	for _, page := range pages {
		byPath[page.Path] = page
	}
	var remaining []string
	for _, path := range pending {
		page, ok := byPath[path]
		if !ok {
			// The page no longer exists: nothing to regenerate.
			continue
		}
		recorded := page.ContentHash
		if mapped, ok := state.Mapping(path); ok && recorded == "" {
			recorded = mapped.ContentHash
		}
		if recorded == "" {
			remaining = append(remaining, path)
			continue
		}
		actual, err := HashFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				remaining = append(remaining, path)
				continue
			}
			return nil, fmt.Errorf("hash %s: %w", path, err)
		}
		if recorded != actual {
			remaining = append(remaining, path)
		}
	}
	return remaining, nil
}
