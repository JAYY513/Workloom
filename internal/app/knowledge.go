package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/config"
	"workloom/internal/knowledge"
	"workloom/internal/storage"
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

// KnowledgeStatusView is the layer's freshness report (M5.3, 方案 §12.5). The
// status field is the contract CI reads through the exit code: fresh (0),
// stale (10) or missing (11).
type KnowledgeStatusView struct {
	Status string `json:"status"`
	Layer  string `json:"layer"`
	Reason string `json:"reason,omitempty"`
	// Baseline is the layer baseline from the generator's state. It is empty
	// until a generator writes state.json: the layer has no baseline of its own,
	// and the per-page `source_commit` values are what a verdict then rests on.
	Baseline string `json:"baseline,omitempty"`
	Head     string `json:"head,omitempty"`
	Branch   string `json:"branch,omitempty"`
	// IndexReady reports whether the file index has been built (M5.2).
	IndexReady bool `json:"index_ready"`
	// IndexFiles is the index's file count when it has been built.
	IndexFiles int `json:"index_files,omitempty"`
	// Pages is the page-layer size; zero means the layer has not been generated.
	Pages int `json:"pages"`
	// Generator is the configured generator, if any.
	Generator string `json:"generator,omitempty"`
	// Adapter is the built-in adapter backing that generator, when it names one
	// (方案 §12.6); AdapterInstalled reports whether the tool it drives is on
	// PATH.
	Adapter          string   `json:"adapter,omitempty"`
	AdapterInstalled bool     `json:"adapter_installed,omitempty"`
	AdapterDetail    string   `json:"adapter_detail,omitempty"`
	ChangedFiles     int      `json:"changed_files"`
	Affected         []string `json:"affected_pages"`
	Unverifiable     []string `json:"unverifiable_pages,omitempty"`
	// FreshPages counts pages with a verdict of fresh.
	FreshPages int `json:"fresh_pages"`
}

// Status constants for KnowledgeStatusView.Status; the CLI maps them onto the
// reserved exit codes.
const (
	KnowledgeFresh   = "fresh"
	KnowledgeStale   = "stale"
	KnowledgeMissing = "missing"
)

// KnowledgeStatus reports whether the page layer still describes the working
// tree (M5.3). A project without pages is `missing`, not an error: not having a
// generator is a supported degradation (方案 §12.6). Git failures are errors —
// §12.5 forbids answering "fresh" when the baseline is unknown.
func (s *Service) KnowledgeStatus(ctx context.Context) (KnowledgeStatusView, error) {
	md, err := s.project()
	if err != nil {
		return KnowledgeStatusView{}, err
	}
	view := KnowledgeStatusView{Layer: "pages", Affected: []string{}}
	if md.Config != nil {
		view.Generator = strings.TrimSpace(md.Config.KnowledgeGenerator)
	}
	if snapshot, err := knowledge.LoadSnapshot(s.Root); err == nil {
		view.IndexReady = true
		view.IndexFiles = snapshot.Stats.TotalFiles
	} else if !os.IsNotExist(err) {
		return KnowledgeStatusView{}, Invalidf(KindKnowledge, nil, "knowledge status: %v", err)
	}

	report, err := knowledge.Scan(s.Root, s.pageRoots(md))
	if err != nil {
		return KnowledgeStatusView{}, Internalf("knowledge status: %v", err)
	}
	view.Pages = len(report.Pages)
	if report.Errors() > 0 {
		return KnowledgeStatusView{}, Invalidf(KindKnowledge, report.Problems,
			"knowledge status: the page layer has %d invalid page(s)", report.Errors())
	}
	state, adapterName, err := s.knowledgeState(md)
	if err != nil {
		return KnowledgeStatusView{}, err
	}
	view.Adapter = adapterName
	if adapterName != "" {
		adapter, ok := knowledge.AdapterByName(adapterName)
		if !ok {
			return KnowledgeStatusView{}, Internalf("knowledge status: adapter %q disappeared", adapterName)
		}
		availability := adapter.Probe(ctx)
		view.AdapterInstalled = availability.Installed
		view.AdapterDetail = availability.Detail
	}
	if state != nil {
		view.Baseline = state.Baseline.Commit
	}
	if view.Pages == 0 {
		view.Status = KnowledgeMissing
		view.Reason = "no page layer: no generator is installed or configured (方案 §12.6)"
		return view, nil
	}
	head, branch, err := knowledge.Head(s.Root)
	if err != nil {
		return KnowledgeStatusView{}, Preconditionf("knowledge status: %v", err)
	}
	view.Head, view.Branch = head, branch
	freshness, err := knowledge.Evaluate(s.Root, report.Pages, state)
	if err != nil {
		if errors.Is(err, knowledge.ErrNoGit) {
			return KnowledgeStatusView{}, Preconditionf("knowledge status: %v", err)
		}
		return KnowledgeStatusView{}, Internalf("knowledge status: %v", err)
	}
	view.ChangedFiles = freshness.ChangedFiles
	view.Affected = freshness.Affected()
	view.Unverifiable = freshness.Unverifiable()
	for _, page := range freshness.Pages {
		if !page.Stale && page.Baseline != "" {
			view.FreshPages++
		}
	}
	view.Status = KnowledgeFresh
	if len(view.Affected) > 0 || len(view.Unverifiable) > 0 {
		view.Status = KnowledgeStale
	}
	switch {
	case len(view.Affected) > 0 && len(view.Unverifiable) > 0:
		view.Reason = fmt.Sprintf("%d page(s) affected by %d changed file(s); %d page(s) have no sources or baseline, so they cannot be called fresh",
			len(view.Affected), view.ChangedFiles, len(view.Unverifiable))
	case len(view.Affected) > 0:
		view.Reason = fmt.Sprintf("%d page(s) affected by %d changed file(s)", len(view.Affected), view.ChangedFiles)
	case len(view.Unverifiable) > 0:
		view.Reason = fmt.Sprintf("%d page(s) have no sources or baseline, so they cannot be called fresh", len(view.Unverifiable))
	default:
		view.Reason = fmt.Sprintf("all %d page(s) match the baseline %s", view.Pages, shortCommit(view.Baseline))
	}
	return view, nil
}

// KnowledgeRefreshRequest selects what a refresh asks the generator to rebuild.
type KnowledgeRefreshRequest struct {
	// Full regenerates every page; otherwise only the affected ones.
	Full bool
	// Force overrides the two protections the layer enforces: `protected: true`
	// and hand-edited pages. Overridden pages are still reported (M5.4).
	Force bool
}

// KnowledgeRefreshView reports one refresh attempt.
type KnowledgeRefreshView struct {
	Scope string   `json:"scope"`
	Pages []string `json:"pages"`
	// Resumed is true when this run continued an interrupted one: its page list
	// is the interrupted run's remainder, with the pages that had already been
	// written left out.
	Resumed bool `json:"resumed,omitempty"`
	// Checkpoint is the run file this refresh wrote (.devsys/knowledge/run.json).
	Checkpoint string `json:"checkpoint,omitempty"`
	Generator  string `json:"generator,omitempty"`
	ScopeFile  string `json:"scope_file,omitempty"`
	Ran        bool   `json:"ran"`
	Reason     string `json:"reason,omitempty"`
	Output     string `json:"output,omitempty"`
	// Missing is true when there is nothing here to regenerate: no page layer,
	// or no generator configured. The CLI reports it with the `missing` exit
	// code instead of pretending a refresh happened (方案 §12.6).
	Missing bool `json:"missing,omitempty"`
	// Skipped lists the pages the layer refused to regenerate and why (M5.4):
	// `protected: true` locks, a hand-edited page's content is not the
	// generator's to replace. With Force they were regenerated anyway and each
	// entry says so.
	Skipped []knowledge.Skip `json:"skipped,omitempty"`
	// Finalized reports that the generator recorded a new baseline.
	Finalized bool `json:"finalized,omitempty"`
	// AwaitingGeneration lists the pages the generator cannot write by itself:
	// RepoWiki writes pages through its skill, so its CLI alone leaves them for
	// an agent (方案 §12.6).
	AwaitingGeneration []string `json:"awaiting_generation,omitempty"`
	// Failed reports a generator that ran and did not finish cleanly.
	Failed bool `json:"failed,omitempty"`
	// Error is the generator failure, when Failed.
	Error string `json:"error,omitempty"`
	// Status is the freshness report taken after a successful run.
	Status *KnowledgeStatusView `json:"status,omitempty"`
}

// KnowledgeRefresh hands the generator the pages that need regenerating
// (方案 §12.5/§12.6). Without a generator there is nothing to run: the answer is
// the documented degradation, and the caller reports it with the `missing`
// exit code instead of pretending the work happened.
func (s *Service) KnowledgeRefresh(ctx context.Context, req KnowledgeRefreshRequest) (KnowledgeRefreshView, error) {
	md, err := s.project()
	if err != nil {
		return KnowledgeRefreshView{}, err
	}
	view := KnowledgeRefreshView{Scope: knowledge.ScopeAffected, Pages: []string{}}
	if req.Full {
		view.Scope = knowledge.ScopeFull
	}
	if md.Config != nil {
		view.Generator = strings.TrimSpace(md.Config.KnowledgeGenerator)
	}
	status, err := s.KnowledgeStatus(ctx)
	if err != nil {
		return KnowledgeRefreshView{}, err
	}
	if status.Pages == 0 {
		view.Missing = true
		view.Reason = "no page layer: no generator is installed or configured (方案 §12.6)"
		return view, nil
	}
	report, err := knowledge.Scan(s.Root, s.pageRoots(md))
	if err != nil {
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	state, adapterName, err := s.knowledgeState(md)
	if err != nil {
		return KnowledgeRefreshView{}, err
	}

	// What this run would ask for on its own: every page (full) or the pages a
	// change touches (affected, plus the pages nobody can attribute).
	var candidates []string
	if req.Full {
		for _, page := range report.Pages {
			candidates = append(candidates, page.Path)
		}
	} else {
		freshness, err := knowledge.Evaluate(s.Root, report.Pages, state)
		if err != nil {
			return KnowledgeRefreshView{}, Preconditionf("knowledge refresh: %v", err)
		}
		candidates = freshness.Affected()
		// A page the layer cannot attribute has to be regenerated too: leaving
		// it alone would keep a claim nobody can check in place.
		candidates = append(candidates, freshness.Unverifiable()...)
		sort.Strings(candidates)
	}

	// An interrupted run left a checkpoint. Its unfinished pages join this run,
	// so the refresh continues instead of restarting: pages already written are
	// dropped by comparing their content against the record (方案 §12.5).
	var resumedPages []string
	if checkpoint, err := knowledge.LoadCheckpoint(s.Root); err == nil {
		view.Resumed = true
		pending := append([]string{}, checkpoint.Pages...)
		pending = append(pending, candidates...)
		sort.Strings(pending)
		pending = compact(pending)
		remaining, err := knowledge.ResumePending(s.Root, report.Pages, state, pending)
		if err != nil {
			return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
		}
		candidates = remaining
		// Whether the interruption is finished or the pages simply vanished
		// changes what the report should say, so the checkpoint's own list is
		// kept for the message.
		resumedPages = checkpoint.Pages
	} else if !knowledge.MissingCheckpoint(err) {
		return KnowledgeRefreshView{}, Invalidf(KindKnowledge, nil, "knowledge refresh: %v", err)
	}

	// Protection comes last, so it covers the resumed pages too: a page the
	// generator must not touch stays out of the scope it is handed, and the
	// report names every page the layer kept out of harm's way (方案 §12.6).
	if len(candidates) > 0 {
		byPath := map[string]*knowledge.Page{}
		for _, page := range report.Pages {
			byPath[page.Path] = page
		}
		var pages []*knowledge.Page
		for _, path := range candidates {
			if page, ok := byPath[path]; ok {
				pages = append(pages, page)
			}
		}
		skips, err := knowledge.Skips(s.Root, pages, state, req.Force)
		if err != nil {
			return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
		}
		view.Skipped = skips
		if !req.Force && len(skips) > 0 {
			skip := map[string]bool{}
			for _, entry := range skips {
				skip[entry.Path] = true
			}
			kept := candidates[:0:0]
			for _, path := range candidates {
				if !skip[path] {
					kept = append(kept, path)
				}
			}
			candidates = kept
		}
	}
	view.Pages = candidates

	// Nothing left to hand over — in either mode, and whether because the tree
	// matches, because the interrupted run had already written everything, or
	// because protection kept every page out of the generator's reach. Running
	// the generator with an empty scope would report work that did not happen.
	if len(view.Pages) == 0 {
		switch {
		case len(view.Skipped) > 0:
			view.Reason = fmt.Sprintf("nothing to regenerate: %d page(s) are protected or hand-edited (use --force to override them)",
				len(view.Skipped))
		case view.Resumed && !pagesPresent(report.Pages, resumedPages):
			view.Reason = "nothing to regenerate: the interrupted run's pages are no longer in the file tree"
		case view.Resumed:
			view.Reason = "nothing to regenerate: the interrupted run had already written every page"
		default:
			view.Reason = "nothing to regenerate: every page matches the working tree"
		}
		if view.Resumed {
			if err := knowledge.ClearCheckpoint(s.Root); err != nil {
				return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
			}
		}
		return view, nil
	}
	if view.Generator == "" {
		view.Missing = true
		view.Reason = fmt.Sprintf("%d page(s) need regenerating, but no generator is configured (knowledge_generator): the page layer is not generated (方案 §12.6)",
			len(view.Pages))
		return view, nil
	}

	// One refresh at a time (M5.5): the mutex is an OS lock, so a killed run
	// leaves no lock behind — only its checkpoint, which is what makes the next
	// run a resume instead of a restart. It is taken only now, when there is
	// work to guard.
	release, err := knowledge.LockRefresh(s.Root)
	if err != nil {
		if errors.Is(err, storage.ErrLocked) {
			return KnowledgeRefreshView{}, Preconditionf(
				"knowledge refresh: another refresh is already running%s; wait for it or remove %s if it died",
				refreshHolder(s.Root), knowledge.RunFile)
		}
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	defer func() { _ = release() }()

	argv, err := knowledge.GeneratorCommand(view.Generator, knowledge.ScopeFile, s.Root)
	if err != nil {
		return KnowledgeRefreshView{}, Usagef("knowledge_generator: %v", err)
	}
	scope := knowledge.Scope{
		FormatVersion: knowledge.FormatVersion,
		Project:       s.Root,
		Mode:          view.Scope,
		Head:          status.Head,
		Baseline:      status.Baseline,
		Pages:         view.Pages,
	}
	scopeFile, err := knowledge.WriteScope(s.Root, scope)
	if err != nil {
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	view.ScopeFile = scopeFile
	view.Ran = true
	now := s.now()
	checkpoint := &knowledge.Checkpoint{
		SchemaVersion: knowledge.RunSchemaVersion,
		PID:           os.Getpid(),
		Host:          hostname(),
		Phase:         knowledge.PhaseGenerating,
		Mode:          view.Scope,
		Generator:     view.Generator,
		ScopeFile:     scopeFile,
		StartedAt:     now,
		UpdatedAt:     now,
		Pages:         view.Pages,
	}
	checkpointFile, err := knowledge.WriteCheckpoint(s.Root, checkpoint)
	if err != nil {
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	view.Checkpoint = checkpointFile
	output, err := s.generate(ctx, adapterName, view.Generator, argv, view.Pages, &view)
	view.Output = output
	if err != nil {
		checkpoint.Phase = knowledge.PhaseFailed
		checkpoint.Error = err.Error()
		checkpoint.UpdatedAt = s.now()
		_, _ = knowledge.WriteCheckpoint(s.Root, checkpoint)
		view.Failed = true
		view.Error = err.Error()
		return view, Preconditionf("knowledge refresh: generator %q failed: %v", view.Generator, err)
	}
	checkpoint.Phase = knowledge.PhaseFinished
	checkpoint.Completed = view.Pages
	checkpoint.UpdatedAt = s.now()
	if _, err := knowledge.WriteCheckpoint(s.Root, checkpoint); err != nil {
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	after, err := s.KnowledgeStatus(ctx)
	if err != nil {
		return view, err
	}
	view.Status = &after
	if err := knowledge.ClearCheckpoint(s.Root); err != nil {
		return KnowledgeRefreshView{}, Internalf("knowledge refresh: %v", err)
	}
	return view, nil
}

// pagesPresent reports whether any of the wanted pages is among the scanned
// ones.
func pagesPresent(scanned []*knowledge.Page, wanted []string) bool {
	if len(wanted) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, page := range scanned {
		have[page.Path] = true
	}
	for _, path := range wanted {
		if have[path] {
			return true
		}
	}
	return false
}

// compact removes duplicates from a sorted list.
func compact(sorted []string) []string {
	out := sorted[:0:0]
	seen := ""
	for i, item := range sorted {
		if i > 0 && item == seen {
			continue
		}
		seen = item
		out = append(out, item)
	}
	return out
}

// refreshHolder names the run that holds the refresh mutex. When its checkpoint
// is missing or unreadable — the holder may not have written it yet, or a
// previous run's file was removed — the answer says so instead of pretending
// nobody holds the lock.
func refreshHolder(root string) string {
	checkpoint, err := knowledge.LoadCheckpoint(root)
	switch {
	case knowledge.MissingCheckpoint(err):
		return " (holder unknown: its checkpoint is not written yet)"
	case err != nil:
		return " (holder unknown: the checkpoint cannot be read)"
	case checkpoint.PID == 0:
		return " (holder unknown: the checkpoint records no pid)"
	default:
		return fmt.Sprintf(" (pid %d on %s since %s)", checkpoint.PID, checkpoint.Host, checkpoint.StartedAt.Format(time.RFC3339))
	}
}

// hostname is the checkpoint's host field; an unknown host is recorded as an
// empty string rather than as a failure.
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// generatorOutputLimit caps what a generator's output can cost this process:
// a generator that streams without end must not take the report (or the
// caller's memory) down with it.
const generatorOutputLimit = 256 << 10

// generate runs the configured generator: through its adapter when the value
// names one (方案 §12.6), otherwise as a command implementing the contract.
func (s *Service) generate(ctx context.Context, adapterName, configured string, argv, pages []string, view *KnowledgeRefreshView) (string, error) {
	if adapterName == "" {
		return s.runGenerator(ctx, argv)
	}
	adapter, ok := knowledge.AdapterByName(adapterName)
	if !ok {
		return "", Usagef("knowledge_generator %q names an unknown adapter", configured)
	}
	result, err := adapter.Generate(ctx, knowledge.GenerateRequest{
		Root: s.Root, ScopeFile: view.ScopeFile, Pages: pages,
	})
	view.Finalized = result.Finalized
	view.AwaitingGeneration = result.AwaitingGeneration
	if result.Note != "" {
		view.Reason = result.Note
	}
	return result.Output, err
}

// runGenerator runs the configured generator in the project root and returns
// its combined output. The process is a child of this command: a refresh is
// interactive, unlike a dispatched attempt, so waiting for it is the point.
func (s *Service) runGenerator(ctx context.Context, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.Root
	// One writer for both streams: os/exec then uses a single pipe, so the
	// bounded buffer needs no locking.
	out := &limitWriter{limit: generatorOutputLimit}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	return out.String(), err
}

// limitWriter keeps the first limit bytes and says so when it truncates.
type limitWriter struct {
	buf       strings.Builder
	limit     int
	truncated bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if room <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		w.buf.Write(p[:room])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *limitWriter) String() string {
	if w.truncated {
		return w.buf.String() + fmt.Sprintf("\n[输出超过 %d 字节，已截断]", w.limit)
	}
	return w.buf.String()
}

// shortCommit renders a commit for messages, tolerating an empty baseline.
func shortCommit(commit string) string {
	if commit == "" {
		return "(none recorded)"
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
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

// knowledgeState loads the layer's own state and layers the configured
// generator's imported mapping under it (方案 §12.6). The second return value is
// the generator's name when a built-in adapter supplied the mapping.
//
// The import is what makes a generator that keeps `sources` in its own state —
// RepoWiki does — attributable at all: without it those pages are unverifiable
// by construction.
func (s *Service) knowledgeState(md *config.Metadata) (*knowledge.State, string, error) {
	local, err := knowledge.LoadState(s.Root)
	if err != nil && !knowledge.MissingState(err) {
		return nil, "", Invalidf(KindKnowledge, nil, "knowledge state: %v", err)
	}
	generator := ""
	if md != nil && md.Config != nil {
		generator = strings.TrimSpace(md.Config.KnowledgeGenerator)
	}
	adapter, ok := knowledge.AdapterByName(generator)
	if !ok {
		return local, "", nil
	}
	imported, err := adapter.Import(s.Root)
	if err != nil {
		return nil, "", Internalf("knowledge import (%s): %v", adapter.Name(), err)
	}
	return knowledge.MergeStates(local, imported), adapter.Name(), nil
}

// pageRoots resolves the page roots: the project's configuration wins, and the
// built-in candidates are the fallback for a project that never configured any.
// Only roots that exist are returned — a candidate that is absent means a
// project that does not use that generator, not a problem.
func (s *Service) pageRoots(md *config.Metadata) []string {
	if md != nil && md.Config != nil && len(md.Config.KnowledgePages) > 0 {
		return md.Config.KnowledgePages
	}
	return knowledge.ExistingRoots(s.Root, knowledge.DefaultRoots)
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
