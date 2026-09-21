// Policy cache and last-known-good resolution (实施计划 M3.5, 方案 §5.3).
//
// Every resolve re-validates the current policy file. A successful parse
// refreshes the project-local last-known-good snapshot
// (.devsys/.cache/workflows/<id>.md); a failing parse falls back to it so
// read paths keep working while dispatch is blocked on the configuration
// error.
package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/JAYY513/Workloom/internal/storage"
)

// Resolution sources.
const (
	SourceCurrent       = "current"
	SourceLastKnownGood = "last-known-good"
)

// lastKnownGoodRel is the snapshot directory inside .devsys/ (local cache:
// deletable, rebuildable, never committed).
const lastKnownGoodRel = ".cache/workflows"

// Resolution is what a policy read produced.
type Resolution struct {
	ID     string
	Policy *Policy
	// Source is SourceCurrent or SourceLastKnownGood.
	Source string
	// Issue describes why the current file could not be used; nil when the
	// current file was valid.
	Issue *Issue
}

// Resolve reads one policy for a consumer. The current file is parsed on
// every call (no mtime trust): success refreshes the last-known-good
// snapshot; failure falls back to the snapshot and reports the current
// failure. Without a usable snapshot both paths fail closed.
func Resolve(ctx context.Context, root, id string) (Resolution, error) {
	if !policyIDPattern.MatchString(id) {
		return Resolution{}, fmt.Errorf("workflow: invalid policy id %q", id)
	}
	rel := "workflows/" + id + ".md"
	data, readErr := os.ReadFile(filepath.Join(root, storage.DevsysDirName, filepath.FromSlash(rel)))
	if readErr == nil {
		policy, issues := Parse(rel, data)
		if policy != nil {
			refreshLastKnownGood(ctx, root, id, data)
			return Resolution{ID: id, Policy: policy, Source: SourceCurrent}, nil
		}
		issue := firstError(issues)
		if policy, ok := cachedPolicy(root, id); ok {
			return Resolution{ID: id, Policy: policy, Source: SourceLastKnownGood, Issue: &issue}, nil
		}
		return Resolution{}, fmt.Errorf("workflow policy %q is invalid: %s (no last-known-good retained)", id, issue.String())
	}
	if !errors.Is(readErr, fs.ErrNotExist) {
		return Resolution{}, fmt.Errorf("workflow policy %q: read: %w", id, readErr)
	}
	if policy, ok := cachedPolicy(root, id); ok {
		issue := Issue{File: rel, Severity: SeverityError, Reason: "missing; using last-known-good"}
		return Resolution{ID: id, Policy: policy, Source: SourceLastKnownGood, Issue: &issue}, nil
	}
	return Resolution{}, fmt.Errorf("workflow policy %q not found under .devsys/workflows/", id)
}

// cachedPolicy parses the last-known-good snapshot; a corrupt snapshot counts
// as absent, never as a fallback candidate.
func cachedPolicy(root, id string) (*Policy, bool) {
	data, err := os.ReadFile(lastKnownGoodPath(root, id))
	if err != nil {
		return nil, false
	}
	policy, _ := Parse("workflows/"+id+".md", data)
	if policy == nil {
		return nil, false
	}
	return policy, true
}

func lastKnownGoodPath(root, id string) string {
	return filepath.Join(root, storage.DevsysDirName, filepath.FromSlash(lastKnownGoodRel), id+".md")
}

// refreshLastKnownGood stores raw as the snapshot for id. The refresh is best
// effort by design: the cache is rebuildable local material and never carries
// correctness, so a failed write is not surfaced to the resolver.
func refreshLastKnownGood(ctx context.Context, root, id string, raw []byte) {
	path := lastKnownGoodPath(root, id)
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, raw) {
		return
	}
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		return
	}
	_ = st.Write(ctx, func(tx *storage.Tx) error {
		return tx.Put(lastKnownGoodRel+"/"+id+".md", raw, storage.ExpectAny())
	})
}

// firstError returns the first error-severity issue, falling back to the
// first issue of any severity.
func firstError(issues []Issue) Issue {
	for _, is := range issues {
		if is.Severity == SeverityError {
			return is
		}
	}
	if len(issues) > 0 {
		return issues[0]
	}
	return Issue{Reason: "invalid policy"}
}
