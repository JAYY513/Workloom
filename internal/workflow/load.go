package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"workloom/internal/storage"
)

// FileResult is the outcome for one entry below .devsys/workflows/.
type FileResult struct {
	// File is the slash-separated path below .devsys/, e.g.
	// "workflows/quick-fix.md".
	File string `json:"file"`
	// Policy is non-nil only when the file parsed without error-severity
	// issues.
	Policy *Policy `json:"-"`
	// Issues are the located errors and warnings this file produced.
	Issues []Issue `json:"issues,omitempty"`
}

// Load reads and validates every policy under .devsys/workflows/ below root.
// It reads plainly — no lock, no recovery, no writes — so it also works on
// state write commands must refuse (same contract as config.Diagnose).
// Entries are returned in file name order; non-.md entries are reported as
// warnings, not failures.
func Load(root string) []FileResult {
	dir := filepath.Join(root, storage.DevsysDirName, "workflows")
	items, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		// A missing policy directory is broken layout, not an empty policy
		// set: `config check` reports missing managed files the same way.
		return []FileResult{{File: "workflows/", Issues: []Issue{{
			File: "workflows/", Reason: "missing; run `devsys init` to create it", Severity: SeverityError,
		}}}}
	}
	if err != nil {
		return []FileResult{{File: "workflows/", Issues: []Issue{{
			File: "workflows/", Reason: fmt.Sprintf("read: %v", err), Severity: SeverityError,
		}}}}
	}
	results := make([]FileResult, 0, len(items))
	for _, item := range items {
		rel := "workflows/" + item.Name()
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".md") {
			results = append(results, FileResult{File: rel, Issues: []Issue{{
				File: rel, Reason: "ignored: only <id>.md policy files are loaded", Severity: SeverityWarning,
			}}})
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			results = append(results, FileResult{File: rel, Issues: []Issue{{
				File: rel, Reason: fmt.Sprintf("read: %v", err), Severity: SeverityError,
			}}})
			continue
		}
		policy, issues := Parse(rel, data)
		results = append(results, FileResult{File: rel, Policy: policy, Issues: issues})
	}
	return results
}
