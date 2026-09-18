// Package workitem — small shared helpers used by claim.go and state.go.
// Kept here so the package owns the imports (os, sort) without pulling them
// into the public API surface.
package workitem

import (
	"os"
	"sort"
)

// errFileNotExist is the shared sentinel for a missing scheduling dir.
var errFileNotExist = os.ErrNotExist

// osReadDir mirrors os.ReadDir so claim.go doesn't need to import os
// directly (we want to keep imports lean).
func osReadDir(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}

// sortStrings is a thin wrapper so claim.go doesn't import sort.
func sortStrings(s []string) { sort.Strings(s) }

// ensureEventsDir creates the .devsys/events directory if absent. Mutating
// paths (Claim/Heartbeat/Release) must call this before opening a
// storage.Write; storage.Write does not create the parent of a JSONL shard,
// so the append fails on a fresh project.
func ensureEventsDir(devsysDir string) error {
	return os.MkdirAll(devsysDir+"/events", 0o755)
}
