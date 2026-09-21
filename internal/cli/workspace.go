package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/sitestatic"
	"workloom/internal/storage"
	"workloom/internal/view"
)

// runWorkspace routes the workspace view family (方案 §17): the read-only
// aggregation behind `workspace build --static` (M7.2) and `workspace serve`
// (M7.3). The execution workspace is `devsys worktree` (M6.2); 方案 §17 keeps
// the two concepts apart and so do their commands.
func runWorkspace(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`devsys workspace` needs a subcommand: view | build | serve") {
		return nil
	}
	switch rest[0] {
	case "view":
		return runWorkspaceView(stdout, opts, rest[1:])
	case "build":
		return runWorkspaceBuild(stdout, opts, rest[1:])
	case "serve":
		return runWorkspaceServe(stdout, opts, rest[1:])
	default:
		return errUsage("unknown `devsys workspace` subcommand %q", rest[0])
	}
}

// runWorkspaceBuild renders the read-only view as an offline static site
// (M7.2, 方案 §17 两种形态之一). It reads through view.Build — the shared
// lock is never created, transactions are never recovered — and the only
// writes are the site files below --out.
func runWorkspaceBuild(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workspace build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	static := fs.Bool("static", false, "render the offline static site")
	out := fs.String("out", "", "output directory (default .devsys/dist/site/)")
	limit := fs.Int("limit", view.DefaultLimit, "max entries per list section (runs, records, pages)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`devsys workspace build --static [--out DIR] [--limit N]`")
	}
	if !*static {
		return errUsage("`devsys workspace build` needs --static (the only site form in M7.2)")
	}
	if *limit <= 0 {
		return errUsage("`--limit` must be positive")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	outDir, err := sitestatic.ResolveOut(svc.Root, *out)
	if err != nil {
		return errUsage("workspace build: %v", err)
	}
	model, err := view.Build(context.Background(), svc.Root, view.Options{Limit: *limit})
	if err != nil {
		if errors.Is(err, storage.ErrNotInitialized) {
			return errPrecondition("workspace build: %v", err)
		}
		return errInternal("workspace build: %v", err)
	}
	generatedAt := time.Now().UTC().Truncate(time.Second)
	pages, err := sitestatic.Build(model, outDir, generatedAt)
	if err != nil {
		return errInternal("workspace build: %v", err)
	}
	rel, relErr := filepath.Rel(svc.Root, outDir)
	if relErr != nil {
		rel = outDir
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK          bool          `json:"ok"`
			Out         string        `json:"out"`
			Pages       []string      `json:"pages"`
			GeneratedAt string        `json:"generated_at"`
			Baseline    view.Baseline `json:"baseline"`
			TrustState  string        `json:"trust_state"`
		}{OK: true, Out: rel, Pages: pages, GeneratedAt: generatedAt.Format(time.RFC3339), Baseline: model.Baseline, TrustState: model.Trust.State})
	}
	if !opts.quiet {
		summary := fmt.Sprintf("site: %s (%d pages) — baseline %s (%s)", rel, len(pages), shortSHA(model.Baseline.Commit), model.Baseline.Branch)
		if !model.Baseline.Available {
			summary = fmt.Sprintf("site: %s (%d pages) — baseline unavailable", rel, len(pages))
		}
		fmt.Fprintln(stdout, summary)
		if model.Trust.State != view.TrustOK {
			note := model.Trust.State
			if model.Trust.Note != "" {
				note += " — " + model.Trust.Note
			}
			fmt.Fprintf(stdout, "trust: %s\n", note)
		}
	}
	return nil
}

// runWorkspaceView assembles and prints the read-only view (M7.1). It writes
// nothing: the model names the files it read and the baseline commit, and an
// untrusted state (pending transactions) renders the reason instead of
// business facts (方案 §15.4).
func runWorkspaceView(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workspace view", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", view.DefaultLimit, "max entries per list section (runs, records, pages)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`devsys workspace view` takes [--limit N] only")
	}
	if *limit <= 0 {
		return errUsage("`--limit` must be positive")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	model, err := view.Build(context.Background(), svc.Root, view.Options{Limit: *limit})
	if err != nil {
		if errors.Is(err, storage.ErrNotInitialized) {
			return errPrecondition("workspace view: %v", err)
		}
		return errInternal("workspace view: %v", err)
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			view.Model
		}{OK: true, Model: *model})
	}
	if !opts.quiet {
		renderWorkspaceView(stdout, model)
	}
	return nil
}

// renderWorkspaceView prints the view as one line per fact, the shape the
// other read commands use. It summarizes; the JSON form is the contract.
func renderWorkspaceView(stdout io.Writer, m *view.Model) {
	p := m.Project
	fmt.Fprintf(stdout, "project: %s (%s) — %s\n", p.Name, p.ID, p.Status)
	if p.CurrentPhase != "" {
		fmt.Fprintf(stdout, "  phase: %s\n", p.CurrentPhase)
	}
	if m.Baseline.Available {
		line := fmt.Sprintf("  baseline: %s (%s)", shortSHA(m.Baseline.Commit), m.Baseline.Branch)
		switch {
		case m.Baseline.Dirty == nil:
			line += "  dirty: unknown"
		case *m.Baseline.Dirty:
			line += fmt.Sprintf("  dirty: yes (%d file(s))", m.Baseline.ChangedFiles)
		default:
			line += "  dirty: no"
		}
		if m.Baseline.Reason != "" {
			line += " — " + m.Baseline.Reason
		}
		fmt.Fprintln(stdout, line)
	} else {
		fmt.Fprintf(stdout, "  baseline: unavailable — %s\n", m.Baseline.Reason)
	}
	if p.Summary != "" {
		fmt.Fprintf(stdout, "  state: %s\n", p.Summary)
	}
	for _, milestone := range p.Milestones {
		fmt.Fprintf(stdout, "  milestone: %s %s\n", milestone.ID, milestone.Status)
	}
	fmt.Fprintf(stdout, "  progress: %d item(s) — %s\n", len(m.Progress.Items), countsLine(m.Progress.Counts))
	fmt.Fprintf(stdout, "  readiness: %s\n", m.Progress.Readiness.Verdict)
	for _, risk := range m.Progress.Readiness.Risks {
		if risk.WorkitemID != "" {
			fmt.Fprintf(stdout, "    risk: %s %s: %s\n", risk.Kind, risk.WorkitemID, risk.Detail)
		} else {
			fmt.Fprintf(stdout, "    risk: %s: %s\n", risk.Kind, risk.Detail)
		}
	}
	next := m.Progress.Readiness.Next
	if next.WorkitemID != "" {
		fmt.Fprintf(stdout, "    next: %s %s — %s\n", next.Action, next.WorkitemID, next.Reason)
	} else {
		fmt.Fprintf(stdout, "    next: %s — %s\n", next.Action, next.Reason)
	}
	fmt.Fprintf(stdout, "  runs: %d — %s%s\n", len(m.Runs.Entries), countsOfRuns(m.Runs.Entries), truncatedSuffix(m.Runs.Truncated))
	fmt.Fprintf(stdout, "  records: decisions %d  findings %d  artifacts %d%s\n",
		len(m.Records.Decisions), len(m.Records.Findings), len(m.Records.Artifacts), truncatedSuffix(m.Records.Truncated))
	k := m.Knowledge
	if k.Status == view.KnowledgeMissing {
		fmt.Fprintf(stdout, "  knowledge: missing — %s\n", k.Reason)
		fmt.Fprintf(stdout, "  hint: configure `knowledge_generator`, then run `devsys knowledge refresh --full`\n")
	} else {
		fmt.Fprintf(stdout, "  knowledge: %s — %d page(s)%s%s\n", k.Status, len(k.Pages), truncatedSuffix(k.Truncated), reasonSuffix(k.Reason))
		for _, page := range k.Affected {
			fmt.Fprintf(stdout, "  affected: %s\n", page)
		}
		for _, page := range k.Unverifiable {
			fmt.Fprintf(stdout, "  unverifiable: %s\n", page)
		}
		switch k.Status {
		case view.KnowledgeStale:
			fmt.Fprintf(stdout, "  hint: run `devsys knowledge refresh` to regenerate affected pages\n")
		case view.KnowledgeUnavailable:
			if m.Trust.State != view.TrustPending {
				fmt.Fprintf(stdout, "  hint: run `devsys knowledge status` to see the underlying error\n")
			}
		}
	}
	if m.Trust.State != view.TrustOK {
		fmt.Fprintf(stdout, "  trust: %s — %s\n", m.Trust.State, m.Trust.Note)
	}
	if len(m.Trust.Pending) > 0 {
		fmt.Fprintf(stdout, "  pending: %s\n", strings.Join(m.Trust.Pending, ", "))
		fmt.Fprintf(stdout, "  hint: run `devsys doctor` to inspect before `devsys recover`\n")
	}
	for _, problem := range m.Problems {
		fmt.Fprintf(stdout, "  problem: %s\n", problem)
	}
	fmt.Fprintf(stdout, "  sources: %s\n", strings.Join(m.Sources, ", "))
}

// countsLine renders the work item counts by status in a stable order.
func countsLine(counts map[string]int) string {
	if len(counts) == 0 {
		return "no work items"
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
	}
	return strings.Join(parts, "  ")
}

// countsOfRuns summarizes the runs by status in a stable order.
func countsOfRuns(entries []view.RunEntry) string {
	if len(entries) == 0 {
		return "none"
	}
	counts := map[string]int{}
	for _, entry := range entries {
		counts[entry.Status]++
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
	}
	return strings.Join(parts, "  ")
}

func truncatedSuffix(capped bool) string {
	if capped {
		return "  (truncated; raise --limit for more)"
	}
	return ""
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " — " + reason
}
