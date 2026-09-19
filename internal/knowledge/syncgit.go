package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrNoUpstream reports that the current branch tracks no upstream branch.
// Callers treat it as a handoff blocker (there is nothing to push to or pull
// from), not as a command failure (方案 §14.4).
var ErrNoUpstream = errors.New("no upstream configured for the current branch")

// Upstream returns the full name (remote/branch) of the current branch's
// upstream. Read-only: it resolves the locally recorded @{u}, never fetching.
func Upstream(root string) (string, error) {
	// Structured probe first: no `branch.<name>.remote` entry means no
	// upstream regardless of git's locale. Only when a remote IS configured
	// do we resolve @{u} (which then fails solely on genuinely broken
	// tracking setup, surfaced as a real error).
	branch, err := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	branch = strings.TrimSpace(branch)
	if branch != "" && branch != "HEAD" {
		if _, err := git(root, "config", "--get", "branch."+branch+".remote"); err != nil {
			return "", ErrNoUpstream
		}
	}
	out, err := git(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// AheadBehind counts how far HEAD and its upstream have diverged, using only
// locally recorded refs (no fetch, no network). ahead counts local-only
// commits, behind counts upstream-only commits.
func AheadBehind(root string) (ahead, behind int, err error) {
	out, err := git(root, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("git rev-list --count: unexpected output %q", strings.TrimSpace(out))
	}
	if ahead, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("git rev-list --count: bad ahead %q", fields[0])
	}
	if behind, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("git rev-list --count: bad behind %q", fields[1])
	}
	return ahead, behind, nil
}

// PorcelainEntry is one `git status --porcelain` record.
type PorcelainEntry struct {
	// XY is the two-letter index/worktree status (e.g. "M ", "??", "UU").
	XY string
	// Path is the worktree-relative path (the new name for renames).
	Path string
	// Orig holds the source path for renames/copies, else "".
	Orig string
	// Unmerged is true for the seven unmerged XY codes (DD AU UD UA DU AA UU):
	// a merge both devices touched and git could not reconcile.
	Unmerged bool
}

// unmergedXY is the set git uses for paths with unresolved merge conflicts.
func unmergedXY(xy string) bool {
	switch xy {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

// PorcelainStatus lists the working tree's uncommitted changes without
// touching them. With -z, a rename/copies entry is "XY new\0orig\0": the
// source path is a separate NUL-terminated field the parser must consume.
func PorcelainStatus(root string) ([]PorcelainEntry, error) {
	out, err := git(root, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	var entries []PorcelainEntry
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if field == "" {
			continue
		}
		if len(field) < 4 {
			return nil, fmt.Errorf("git status --porcelain: malformed entry %q", field)
		}
		xy, path := field[:2], field[3:]
		entry := PorcelainEntry{XY: xy, Path: path, Unmerged: unmergedXY(xy)}
		// Renames/copies carry the source as the following NUL field.
		if xy[0] == 'R' || xy[0] == 'C' {
			i++
			if i >= len(fields) {
				return nil, fmt.Errorf("git status --porcelain: rename %q missing source", path)
			}
			entry.Orig = fields[i]
		}
		entries = append(entries, entry)
	}
	if entries == nil {
		entries = []PorcelainEntry{}
	}
	return entries, nil
}

// MergeHeads names the in-progress merge machinery git currently holds
// (MERGE_HEAD, CHERRY_PICK_HEAD, REBASE_HEAD, REVERT_HEAD, rebase-merge/,
// rebase-apply/): any of them means a merge the operator started and never
// finished. Read-only: existence checks only.
func MergeHeads(root string) ([]string, error) {
	out, err := git(root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	gitDir := strings.TrimSpace(out)
	if gitDir == "" {
		return nil, fmt.Errorf("git rev-parse --absolute-git-dir: empty result")
	}
	var found []string
	for _, name := range []string{
		"MERGE_HEAD", "CHERRY_PICK_HEAD", "REBASE_HEAD", "REVERT_HEAD",
		"rebase-merge", "rebase-apply",
	} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			found = append(found, name)
		}
	}
	if found == nil {
		found = []string{}
	}
	return found, nil
}
