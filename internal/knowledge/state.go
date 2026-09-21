package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/JAYY513/Workloom/internal/storage"
)

// StateSchemaVersion is the only state version this build reads or writes.
const StateSchemaVersion = 1

// Baseline is the commit the page layer describes.
type Baseline struct {
	Commit string `json:"commit"`
	Branch string `json:"branch,omitempty"`
}

// PageState is the mapping entry for one page: the patterns it covers and the
// hash of what the generator last wrote (方案 §12.5). Pages may declare
// `sources` themselves; this mapping is the other half of the contract — a
// generator that keeps its own state (RepoWiki does) supplies them here.
type PageState struct {
	Sources      []string `json:"sources,omitempty"`
	ContentHash  string   `json:"content_hash,omitempty"`
	SourceCommit string   `json:"source_commit,omitempty"`
}

// State is the page layer's bookkeeping: the overall baseline and the page
// mapping (.devsys/knowledge/state.json). It is written by whatever generates
// pages — the generator contract (方案 §12.6) — never by the index scan: a
// baseline that moved with every scan would make stale pages look fresh.
type State struct {
	SchemaVersion int                  `json:"schema_version"`
	Baseline      Baseline             `json:"baseline"`
	Pages         map[string]PageState `json:"pages,omitempty"`
	Generator     string               `json:"generator,omitempty"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// LoadState reads the state file. A missing file is reported as fs.ErrNotExist
// so a caller can distinguish "no page layer yet" from "state is unreadable".
func LoadState(root string) (*State, error) {
	abs := filepath.Join(root, filepath.FromSlash(StateFile))
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("%s: %w", StateFile, err)
	}
	if state.SchemaVersion != StateSchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d is not supported (this build reads %d)",
			StateFile, state.SchemaVersion, StateSchemaVersion)
	}
	return &state, nil
}

// WriteState stores the state file atomically.
func WriteState(root string, state *State) (string, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	abs := filepath.Join(root, filepath.FromSlash(StateFile))
	if err := storage.AtomicWrite(abs, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", StateFile, err)
	}
	return StateFile, nil
}

// Mapping returns the entry for a page path, matching slash-separated paths.
func (s *State) Mapping(pagePath string) (PageState, bool) {
	if s == nil {
		return PageState{}, false
	}
	entry, ok := s.Pages[pagePath]
	return entry, ok
}

// MissingState reports whether the error means "there is no state file".
func MissingState(err error) bool {
	return err != nil && errors.Is(err, fs.ErrNotExist)
}
