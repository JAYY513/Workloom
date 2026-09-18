package knowledge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"workloom/internal/config"
)

// Report is the outcome of validating a set of page roots.
type Report struct {
	// Roots are the roots that were scanned, slash separated and relative to
	// the project root, in scan order.
	Roots []string
	// Missing is true when no roots were given: the page layer has not been
	// generated (方案 §12.6), which is a degradation and not a defect.
	Missing bool
	// Pages are the parsed pages, in path order.
	Pages []*Page
	// Problems are all located problems, both severities.
	Problems []config.Problem
}

// Errors counts the problems that make a page unusable.
func (r Report) Errors() int { return r.count("") }

// Warnings counts the advisory problems.
func (r Report) Warnings() int { return r.count(config.SeverityWarning) }

func (r Report) count(severity string) int {
	n := 0
	for _, p := range r.Problems {
		if p.Severity == severity {
			n++
		}
	}
	return n
}

// ExistingRoots keeps the candidates that exist under root, in order. It backs
// the default page roots: a candidate that is not there is a project that does
// not use that generator, not a problem to report.
func ExistingRoots(root string, candidates []string) []string {
	var found []string
	for _, rel := range candidates {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && info.IsDir() {
			found = append(found, rel)
		}
	}
	return found
}

// Scan parses and validates every page under roots. A root is a
// project-relative directory (scanned recursively for .md files, dot
// directories skipped) or a single page file. Results are deterministic: files
// are visited in sorted order.
func Scan(root string, roots []string) (Report, error) {
	report := Report{}
	if len(roots) == 0 {
		report.Missing = true
		return report, nil
	}
	for _, rel := range roots {
		report.Roots = append(report.Roots, filepath.ToSlash(rel))
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil {
			return Report{}, fmt.Errorf("scan %s: %w", rel, err)
		}
		var files []string
		if info.IsDir() {
			files, err = collectPages(abs)
			if err != nil {
				return Report{}, fmt.Errorf("scan %s: %w", rel, err)
			}
		} else {
			files = []string{abs}
		}
		for _, file := range files {
			pageRel, err := filepath.Rel(root, file)
			if err != nil {
				return Report{}, fmt.Errorf("scan %s: %w", rel, err)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return Report{}, fmt.Errorf("read %s: %w", pageRel, err)
			}
			page, problems := Parse(filepath.ToSlash(pageRel), data)
			report.Pages = append(report.Pages, page)
			report.Problems = append(report.Problems, problems...)
		}
	}
	return report, nil
}

// collectPages lists the Markdown files under dir, sorted and slash
// separated. Dot directories and dot files are skipped: they are tooling, not
// pages.
func collectPages(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != dir && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
