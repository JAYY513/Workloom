package knowledge

import (
	"fmt"
	"os/exec"
	"strings"
)

// Head returns the working tree's commit and branch. A detached HEAD reports
// the literal "HEAD" as the branch: the identity of the tree is the commit,
// and inventing a branch name would be a claim the repository does not make.
func Head(root string) (commit, branch string, err error) {
	out, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	commit = strings.TrimSpace(out)
	out, err = git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	return commit, strings.TrimSpace(out), nil
}

// Changes lists the paths that differ between a commit and the working tree:
// committed changes, uncommitted modifications and deletions, and untracked
// files. Comparing against the working tree rather than HEAD is deliberate —
// a page that describes committed code is stale the moment the tree changes,
// whether or not anyone has committed yet.
func Changes(root, commit string) ([]string, error) {
	if strings.TrimSpace(commit) == "" {
		return nil, fmt.Errorf("no baseline commit recorded")
	}
	changed, err := git(root, "diff", "--name-only", "-z", commit, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, list := range []string{changed, untracked} {
		for _, path := range strings.Split(list, "\x00") {
			path = strings.TrimSpace(path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// PageFreshness is one page's verdict.
type PageFreshness struct {
	Path string `json:"path"`
	// Stale is true when a change since the page's baseline touches a pattern
	// the page covers.
	Stale bool `json:"stale"`
	// Baseline is the commit the verdict compared against; empty when the page
	// has no baseline at all and therefore cannot be called fresh.
	Baseline string `json:"baseline,omitempty"`
	// Reason explains a stale or unverifiable verdict.
	Reason string `json:"reason,omitempty"`
	// AffectedBy lists the changed paths that made the page stale (bounded by
	// the caller's patience: every matching path is recorded).
	AffectedBy []string `json:"affected_by,omitempty"`
}

// Freshness is the layer-level verdict.
type Freshness struct {
	Pages []PageFreshness `json:"pages"`
	// ChangedFiles counts the working-tree changes the verdict considered.
	ChangedFiles int `json:"changed_files"`
}

// Affected lists the pages a change touches.
func (f Freshness) Affected() []string {
	var paths []string
	for _, page := range f.Pages {
		if page.Stale {
			paths = append(paths, page.Path)
		}
	}
	return paths
}

// Unverifiable lists the pages the layer cannot claim anything about: no
// sources to attribute changes to, or no baseline to compare with. They are
// the reason a run reports stale even when nothing matches — "cannot tell" is
// not "fresh".
func (f Freshness) Unverifiable() []string {
	var paths []string
	for _, page := range f.Pages {
		if !page.Stale && page.Baseline == "" {
			paths = append(paths, page.Path)
		}
	}
	return paths
}

// Evaluate compares every page against the working tree. Sources come from the
// page first and from the generator's mapping otherwise (方案 §12.5); the
// baseline is the page's own `source_commit` when it has one, else the
// mapping's, else the layer baseline. Git failures are errors: §12.5 forbids
// degrading to an "unknown baseline".
func Evaluate(root string, pages []*Page, state *State) (Freshness, error) {
	freshness := Freshness{Pages: []PageFreshness{}}
	changes := map[string][]string{}
	loadChanges := func(commit string) ([]string, error) {
		if cached, ok := changes[commit]; ok {
			return cached, nil
		}
		list, err := Changes(root, commit)
		if err != nil {
			return nil, err
		}
		changes[commit] = list
		return list, nil
	}
	for _, page := range pages {
		verdict := PageFreshness{Path: page.Path}
		sources := page.Sources
		var entry PageState
		if mapped, ok := state.Mapping(page.Path); ok {
			entry = mapped
			if len(sources) == 0 {
				sources = mapped.Sources
			}
		}
		baseline := page.SourceCommit
		if baseline == "" {
			baseline = entry.SourceCommit
		}
		if baseline == "" && state != nil {
			baseline = state.Baseline.Commit
		}
		switch {
		case len(sources) == 0:
			verdict.Reason = "no sources: the layer cannot attribute changes to this page"
		case baseline == "":
			verdict.Reason = "no baseline: neither the page nor the state records a commit"
		default:
			verdict.Baseline = baseline
			changed, err := loadChanges(baseline)
			if err != nil {
				return Freshness{}, err
			}
			for _, path := range changed {
				if matchesAny(sources, path) {
					verdict.Stale = true
					verdict.AffectedBy = append(verdict.AffectedBy, path)
				}
			}
			if verdict.Stale {
				verdict.Reason = "changed: " + strings.Join(verdict.AffectedBy, ", ")
			}
		}
		freshness.Pages = append(freshness.Pages, verdict)
	}
	if len(changes) == 1 {
		for _, list := range changes {
			freshness.ChangedFiles = len(list)
		}
	} else {
		seen := map[string]bool{}
		for _, list := range changes {
			for _, path := range list {
				seen[path] = true
			}
		}
		freshness.ChangedFiles = len(seen)
	}
	return freshness, nil
}

// git runs one git command in root and returns stdout. A missing git or a
// failing command is an error the caller must surface (方案 §12.5).
func git(root string, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoGit, err)
	}
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%w: git %s: %s", ErrNoGit, strings.Join(args, " "), message)
	}
	return stdout.String(), nil
}
