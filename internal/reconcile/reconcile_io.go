// Package reconcile — filesystem helpers used by Doctor / DryRun.
//
// These helpers walk .devsys subdirectories without going through
// storage.Read (which would auto-recover and create the lock file).
// They are only used inside Store.Inspect callbacks, so they operate
// against a project root that is guaranteed to exist (storage.Open
// checked .devsys).
package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// listSchedulingEntries returns the filenames (no path) of every .yaml
// file in <devsys>/scheduling/, in ascending lexical order. A missing
// directory is reported as an empty list (no scheduling/ yet → no leases).
func listSchedulingEntries(devsys string) ([]string, error) {
	dir := filepath.Join(devsys, "scheduling")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	return yamlNames(entries), nil
}

// listWorkitemEntries returns the filenames (no path) of every .yaml file
// in <devsys>/workitems/. A missing directory is reported as an empty
// list.
func listWorkitemEntries(devsys string) ([]string, error) {
	dir := filepath.Join(devsys, "workitems")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	return yamlNames(entries), nil
}

// yamlNames keeps only regular files ending in .yaml, sorted.
func yamlNames(entries []fs.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
