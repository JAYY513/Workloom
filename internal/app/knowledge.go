package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workloom/internal/config"
	"workloom/internal/knowledge"
)

// KnowledgePageView summarizes one page for the validate report.
type KnowledgePageView struct {
	Path         string `json:"path"`
	Status       string `json:"status,omitempty"`
	Type         string `json:"type,omitempty"`
	Triggers     int    `json:"triggers"`
	Sources      int    `json:"sources"`
	SourceCommit string `json:"source_commit,omitempty"`
	Protected    bool   `json:"protected,omitempty"`
	Issues       int    `json:"issues,omitempty"`
}

// KnowledgeValidateView is the page layer's validation result. Errors are
// located problems that make a page unusable; warnings are advisories.
type KnowledgeValidateView struct {
	Roots    []string            `json:"roots"`
	Missing  bool                `json:"missing,omitempty"`
	Pages    []KnowledgePageView `json:"pages"`
	Errors   int                 `json:"errors"`
	Warnings int                 `json:"warnings"`
	Issues   []config.Problem    `json:"issues,omitempty"`
}

// KnowledgeStatusView reports the knowledge layer's availability. The index
// layer arrives with M5 (方案 §12.6); until then the honest answer is
// "unavailable, no index", which is exactly the degradation contract — a
// missing generator must not break the rest of the system.
type KnowledgeStatusView struct {
	Status     string   `json:"status"`
	Layer      string   `json:"layer"`
	Configured bool     `json:"configured"`
	Reason     string   `json:"reason"`
	Paths      []string `json:"paths,omitempty"`
}

// KnowledgeStatus reports whether a knowledge index is present under
// .devsys/knowledge/ and what the layer can answer today.
func (s *Service) KnowledgeStatus(ctx context.Context) (KnowledgeStatusView, error) {
	if _, err := s.project(); err != nil {
		return KnowledgeStatusView{}, err
	}
	dir := filepath.Join(s.Root, ".devsys", "knowledge")
	view := KnowledgeStatusView{Status: "unavailable", Layer: "index"}
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil:
		view.Configured = true
		for _, entry := range entries {
			view.Paths = append(view.Paths, filepath.ToSlash(filepath.Join(".devsys/knowledge", entry.Name())))
		}
		view.Reason = "knowledge index directory exists but the index layer arrives with M5 (方案 §12.6); no page contract is enforced yet"
	case os.IsNotExist(err):
		view.Reason = "no .devsys/knowledge/ index: the knowledge layer arrives with M5 (方案 §12.6); queries degrade to record and event tools"
	default:
		return KnowledgeStatusView{}, Internalf("inspect knowledge directory: %v", err)
	}
	return view, nil
}

// KnowledgeValidate checks the page layer's front matter contract (方案 §12.5,
// M5.1). Explicit roots — from the command line or `knowledge_pages` — must
// exist; the built-in candidates that do not exist are skipped, and no page
// layer at all is reported as `missing` rather than as a failure: not having a
// generator is a supported degradation (方案 §12.6). Errors refuse the call
// (exit code 4) with every located problem, warnings included.
func (s *Service) KnowledgeValidate(ctx context.Context, roots []string) (KnowledgeValidateView, error) {
	md, err := s.project()
	if err != nil {
		return KnowledgeValidateView{}, err
	}
	view := KnowledgeValidateView{Roots: []string{}, Pages: []KnowledgePageView{}}
	explicit := true
	if len(roots) == 0 && md.Config != nil {
		roots = md.Config.KnowledgePages
	}
	if len(roots) == 0 {
		explicit = false
		roots = knowledge.ExistingRoots(s.Root, knowledge.DefaultRoots)
	}
	if explicit {
		for _, rel := range roots {
			if err := s.checkPageRoot(rel); err != nil {
				return KnowledgeValidateView{}, err
			}
		}
	}
	report, err := knowledge.Scan(s.Root, roots)
	if err != nil {
		return KnowledgeValidateView{}, Internalf("knowledge validate: %v", err)
	}
	view.Roots = append(view.Roots, report.Roots...)
	view.Missing = report.Missing
	view.Errors = report.Errors()
	view.Warnings = report.Warnings()
	view.Issues = report.Problems
	for _, page := range report.Pages {
		issues := 0
		for _, problem := range report.Problems {
			if problem.File == page.Path {
				issues++
			}
		}
		view.Pages = append(view.Pages, KnowledgePageView{
			Path:         page.Path,
			Status:       page.Status,
			Type:         page.Type,
			Triggers:     len(page.Triggers),
			Sources:      len(page.Sources),
			SourceCommit: page.SourceCommit,
			Protected:    page.Protected,
			Issues:       issues,
		})
	}
	if view.Errors > 0 {
		return view, Invalidf(KindKnowledge, report.Problems,
			"knowledge validate: %d errors, %d warnings", view.Errors, view.Warnings)
	}
	return view, nil
}

// checkPageRoot refuses a page root that is missing or outside the project.
// Both would make the report a claim about files the layer cannot attribute
// to this project.
func (s *Service) checkPageRoot(rel string) error {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return Usagef("knowledge page root must not be empty")
	}
	abs := rel
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.Root, filepath.FromSlash(rel))
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Preconditionf("knowledge page root %s: %v", filepath.ToSlash(rel), err)
	}
	inside, err := filepath.Rel(s.Root, abs)
	if err != nil || outsideRoot(inside) {
		return Preconditionf("knowledge page root %s is outside the project", filepath.ToSlash(rel))
	}
	if !info.IsDir() && !strings.HasSuffix(abs, ".md") {
		return Usagef("knowledge page root %s must be a directory or a .md page", filepath.ToSlash(rel))
	}
	return nil
}

// outsideRoot reports whether a path relative to the project root escapes it.
func outsideRoot(rel string) bool {
	return filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// KnowledgeScanExclusion is one path the index left out, with its reason.
type KnowledgeScanExclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// KnowledgeScanView reports one index scan (M5.2). The exclusion list travels
// with the result: an index that is smaller than the tree must say why.
type KnowledgeScanView struct {
	File                string                   `json:"file"`
	GeneratedAt         time.Time                `json:"generated_at"`
	Files               int                      `json:"files"`
	TotalSize           int64                    `json:"total_size"`
	Languages           map[string]int           `json:"languages"`
	Excluded            []KnowledgeScanExclusion `json:"excluded"`
	ExcludedDirectories []string                 `json:"excluded_directories"`
	IgnoreFiles         []string                 `json:"ignore_files,omitempty"`
	Rules               []string                 `json:"rules"`
}

// KnowledgeScan builds the index layer's snapshot of the working tree and
// writes it to .devsys/knowledge/snapshot.json (M5.2, 方案 §12.2). It reads
// the tree and git only; nothing else in .devsys/ is touched.
func (s *Service) KnowledgeScan(ctx context.Context) (KnowledgeScanView, error) {
	if _, err := s.project(); err != nil {
		return KnowledgeScanView{}, err
	}
	snapshot, err := knowledge.BuildSnapshot(s.Root, knowledge.ScanOptions{Now: s.Now})
	if err != nil {
		if errors.Is(err, knowledge.ErrNoGit) {
			return KnowledgeScanView{}, Preconditionf("knowledge scan: %v", err)
		}
		return KnowledgeScanView{}, Internalf("knowledge scan: %v", err)
	}
	rel, err := knowledge.WriteSnapshot(s.Root, snapshot)
	if err != nil {
		return KnowledgeScanView{}, Internalf("knowledge scan: %v", err)
	}
	view := KnowledgeScanView{
		File:                rel,
		GeneratedAt:         snapshot.GeneratedAt,
		Files:               snapshot.Stats.TotalFiles,
		TotalSize:           snapshot.Stats.TotalSize,
		Languages:           snapshot.Stats.Languages,
		ExcludedDirectories: snapshot.Excluded.Directories,
		IgnoreFiles:         snapshot.Excluded.IgnoreFiles,
		Rules:               snapshot.Excluded.Rules,
		Excluded:            []KnowledgeScanExclusion{},
	}
	for _, exclusion := range snapshot.Excluded.Files {
		view.Excluded = append(view.Excluded, KnowledgeScanExclusion{Path: exclusion.Path, Reason: exclusion.Reason})
	}
	return view, nil
}
