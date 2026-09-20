package view

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/approval"
	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/knowledge"
	"workloom/internal/next"
	"workloom/internal/reconcile"
	"workloom/internal/storage"
	"workloom/internal/workflow"
)

// recoverCommand is the remediation the readiness section names while pending
// transactions make the state untrustworthy (the wording `devsys next` uses).
const recoverCommand = `devsys recover --actor operator --reason "recover interrupted state"`

// pendingReason explains why business sections render nothing.
const pendingReason = "pending transactions: run `devsys recover` before trusting business state (方案 §15.4)"

// Build assembles the view of the project at root. It reads only — see the
// package doc for the exact contracts. A missing .devsys/ is an error
// (storage.ErrNotInitialized): there is no state to view.
func Build(ctx context.Context, root string, opts Options) (*Model, error) {
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	st, err := storage.Open(root, storage.Options{Now: nowFn})
	if err != nil {
		return nil, err
	}
	// Doctor is the read-only inspector: it reports pending transactions and
	// lease anomalies without creating the lock or recovering anything. It
	// decodes every work item while proposing repairs, so one corrupt file
	// must not hide the whole view: fall back to the diagnosis that does not
	// depend on those files and report the limitation.
	doc, doctorErr := reconcile.Doctor(ctx, root, reconcile.Options{Now: nowFn})
	inspection := ""
	if doctorErr != nil {
		diag, err := st.Diagnose()
		if err != nil {
			return nil, err
		}
		doc = reconcile.InspectionReport{
			Note:                "lease inspection degraded: " + doctorErr.Error(),
			PendingTransactions: diag.Pending,
		}
		inspection = "inspection: " + doctorErr.Error()
	}

	m := newModel()
	m.Trust = Trust{
		State:        TrustOK,
		InspectionOK: doc.InspectionOK,
		Note:         doc.Note,
		Pending:      pendingIDs(doc.PendingTransactions),
	}
	// Git state is observed independently of the managed state, so it is read
	// in every case — including the untrusted one.
	m.Baseline = readBaseline(root)
	if len(doc.PendingTransactions) > 0 {
		m.Trust.State = TrustPending
		emptyBusiness(m)
		m.Progress.Readiness = pendingReadiness(nowFn(), pendingIDs(doc.PendingTransactions))
		m.Sources = collectSources(m)
		return m, nil
	}

	rd := newReader(st)
	var b *builder
	advisory, err := inspectSnapshot(ctx, st, func(r *storage.Reader) error {
		rd.r = r
		b = &builder{root: root, now: nowFn(), limit: limit, rd: rd, doc: doc, m: m, inspection: inspection}
		b.assemble()
		return nil
	})
	if err != nil {
		var pending *storage.PendingTxnError
		if errors.As(err, &pending) {
			// A writer started between Doctor and the read; report it exactly
			// like Doctor would instead of reading half-applied state.
			m.Trust.State = TrustPending
			m.Trust.Pending = pending.IDs
			emptyBusiness(m)
			m.Progress.Readiness = pendingReadiness(nowFn(), pending.IDs)
			m.Sources = collectSources(m)
			return m, nil
		}
		return nil, err
	}
	if advisory {
		m.Trust.State = TrustAdvisory
		m.Trust.Note = strings.TrimSpace(m.Trust.Note + " reads were lock-free: no project lock exists (no writer ever ran), so the snapshot is advisory")
		m.Trust.InspectionOK = false
	}
	m.Problems = sortedUnique(b.problems)
	m.Sources = collectSources(m)
	return m, nil
}

// newModel returns a model with every slice initialized, so a section that
// found nothing encodes as [] rather than null and two builds stay identical.
func newModel() *Model {
	return &Model{
		SchemaVersion: SchemaVersion,
		Project: Project{
			Provenance: Provenance{Sources: []string{}},
			Milestones: []domain.Milestone{},
			Risks:      []string{}, Blockers: []string{}, NextFocus: []string{},
		},
		Trust:   Trust{Pending: []string{}},
		Runs:    Runs{Provenance: Provenance{Sources: []string{}}, Entries: []RunEntry{}},
		Records: Records{Provenance: Provenance{Sources: []string{}}, Decisions: []RecordRef{}, Findings: []RecordRef{}, Artifacts: []RecordRef{}},
		Progress: Progress{
			Provenance: Provenance{Sources: []string{}},
			Counts:     map[string]int{},
			Items:      []Item{},
			Readiness:  next.Report{Verdict: next.VerdictPass, Reasons: []string{}, Risks: []next.Risk{}},
		},
		Knowledge: Knowledge{
			Provenance: Provenance{Sources: []string{}},
			Pages:      []PageEntry{}, Affected: []string{},
		},
		Sources: []string{},
	}
}

// emptyBusiness renders no business facts: while unfinished transactions exist
// they are not valid business state (方案 §15.4). Trust and baseline still
// describe what was observed.
func emptyBusiness(m *Model) {
	m.Project.Degraded = true
	m.Project.Reason = pendingReason
	m.Progress.Degraded = true
	m.Progress.Reason = pendingReason
	m.Records = Records{Provenance: Provenance{Sources: []string{}}, Decisions: []RecordRef{}, Findings: []RecordRef{}, Artifacts: []RecordRef{}}
	m.Knowledge.Status = KnowledgeUnavailable
	m.Knowledge.Reason = pendingReason
	m.Knowledge.Degraded = true
}

// pendingReadiness is the §7.4 verdict while unfinished transactions exist:
// FAIL with the recovery command, the same report `devsys next` produces, so
// the view and the readiness verdict cannot disagree.
func pendingReadiness(now time.Time, ids []string) next.Report {
	txns := make([]storage.PendingTxn, 0, len(ids))
	for _, id := range ids {
		txns = append(txns, storage.PendingTxn{ID: id})
	}
	return next.Evaluate(next.Input{Now: now, PendingTxns: txns, RecoverCommand: recoverCommand})
}

// builder assembles one snapshot inside the shared-lock window. A fresh
// builder is constructed on every invocation of the closure, so nothing
// accumulates if inspectSnapshot ever runs it more than once.
type builder struct {
	root     string
	now      time.Time
	limit    int
	rd       *reader
	doc      reconcile.InspectionReport
	md       *config.Metadata
	m        *Model
	problems []string
	// inspection is the located reason the lease inspection degraded, empty
	// when Doctor answered fully.
	inspection string
}

func (b *builder) assemble() {
	if b.inspection != "" {
		b.problemf("%s", b.inspection)
	}
	b.project()
	items := b.progress()
	b.runs()
	b.records()
	b.knowledge(items)
}

// project fills the blueprint from the managed metadata files plus the
// current-state and milestones files. A broken metadata file is reported in
// Problems and marks the section degraded — a view must show a broken project,
// not fail.
func (b *builder) project() {
	mark := b.rd.mark()
	p := &b.m.Project
	md, problems := config.Diagnose(b.root)
	b.md = md
	for _, problem := range problems {
		b.problemf("%s", problem.String())
	}
	for _, rel := range config.ManagedFiles() {
		if b.rd.exists(rel) {
			b.rd.noteDevsys(rel)
		}
	}
	if md == nil || md.Project == nil {
		p.Degraded = true
		p.Reason = "project metadata is missing or invalid; run `devsys init`"
	} else {
		mp := md.Project
		p.ID, p.Name, p.Description, p.Status = mp.ID, mp.Name, mp.Description, mp.Status
		p.CurrentPhase = mp.CurrentPhase
		p.Goals = mp.Goals
		p.Scope = mp.Scope
		p.Constraints = mp.Constraints
		p.TechStack = mp.TechStack
		p.Milestones = mp.Milestones
		p.CreatedAt, p.UpdatedAt = timePtr(mp.CreatedAt), timePtr(mp.UpdatedAt)
	}
	var cur domain.CurrentStateFile
	if _, err := b.rd.read(config.CurrentStateFile, &cur); err != nil {
		b.problem(err)
		p.Degraded = true
		p.Reason = err.Error()
	} else {
		p.Summary = cur.Summary
		p.Risks, p.Blockers, p.NextFocus = cur.Risks, cur.Blockers, cur.NextFocus
	}
	var ms domain.MilestonesFile
	if _, err := b.rd.read(config.MilestonesFile, &ms); err != nil {
		b.problem(err)
		p.Degraded = true
		if p.Reason == "" {
			p.Reason = err.Error()
		}
	} else if len(ms.Milestones) > 0 {
		p.Milestones = ms.Milestones
	}
	p.Sources = b.rd.since(mark)
}

// progress fills the board: every work item with its scheduling facts, plus
// the §7.4 readiness verdict over the same inputs `devsys next` consumes.
func (b *builder) progress() []*domain.WorkItem {
	mark := b.rd.mark()
	pr := &b.m.Progress
	var failed []string
	names, err := b.rd.list("workitems", ".yaml")
	if err != nil {
		b.problem(err)
		failed = append(failed, err.Error())
	}
	var items []*domain.WorkItem
	for _, name := range names {
		var wi domain.WorkItem
		if _, err := b.rd.read("workitems/"+name, &wi); err != nil {
			b.problem(err)
			failed = append(failed, err.Error())
			continue
		}
		items = append(items, &wi)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, wi := range items {
		pr.Counts[wi.Status]++
		pr.Items = append(pr.Items, itemFrom(wi))
	}
	facts := b.policyFacts(items)
	dead, inflight := b.attemptSignals(items)
	pr.Readiness = next.Evaluate(next.Input{
		Now:              b.now,
		WorkItems:        items,
		Milestones:       b.m.Project.Milestones,
		PendingTxns:      b.doc.PendingTransactions,
		ExpiredLeases:    b.doc.ExpiredLeases,
		OrphanLeases:     b.doc.OrphanLeases,
		UnreadableLeases: b.doc.UnreadableLeases,
		InspectionOK:     b.doc.InspectionOK,
		InspectionNote:   b.doc.Note,
		MetadataProblems: b.metadataProblems(),
		PolicyProblems:   facts.problems,
		PolicyIDs:        facts.ids,
		DefaultPolicy:    b.defaultPolicy(),
		HasBlueprint:     b.md != nil && b.md.Project != nil && strings.TrimSpace(b.md.Project.BlueprintArtifactID) != "",
		DeadAttempts:     dead,
		InFlight:         inflight,
		QualityBlocks:    next.QualityBlocks(items, facts.policyOf(b.defaultPolicy())),
		PendingApprovals: b.pendingApprovals(),
		RecoverCommand:   recoverCommand,
	})
	if len(failed) > 0 {
		pr.Degraded = true
		pr.Reason = strings.Join(failed, "; ")
	}
	pr.Sources = b.rd.since(mark)
	return items
}

// metadataProblems are the located metadata defects the readiness evaluation
// records as risks (same strings `devsys next` uses).
func (b *builder) metadataProblems() []string {
	_, problems := config.Diagnose(b.root)
	out := make([]string, 0, len(problems))
	for _, problem := range problems {
		out = append(out, problem.String())
	}
	return out
}

// policyFacts are the policy reads the readiness section consumes: the ids the
// project declares, the parsed policy per id (nil when the file failed to
// parse) and the located problems. The files are read by workflow.Load (plain
// reads, no lock) — the last-known-good resolver refreshes a cache and so is
// not available to a read-only view.
type policyFacts struct {
	ids      []string
	parsed   map[string]*workflow.Policy
	problems []string
}

// defaultPolicy is the project-level policy declared in .devsys/config.yaml,
// or "" when none is set.
func (b *builder) defaultPolicy() string {
	if b.md == nil || b.md.Config == nil {
		return ""
	}
	return strings.TrimSpace(b.md.Config.DefaultPolicy)
}

// policyOf resolves the policy governing one work item the way `devsys next`
// judges it: the item's own instance first, else the project default.
func (f policyFacts) policyOf(defaultPolicy string) next.PolicySource {
	return func(wi *domain.WorkItem) (string, string, *workflow.Policy) {
		id := defaultPolicy
		if wi.Workflow != nil && wi.Workflow.ID != "" {
			id = wi.Workflow.ID
		}
		if id == "" {
			return "", "", nil
		}
		return id, "workflows/" + id + ".md", f.parsed[id]
	}
}

// policyFacts lists located policy failures, the declared policy ids, and work
// items that reference a policy file that does not exist; the last-known-good
// fallback keeps read paths usable while dispatch stays blocked (M3.5).
func (b *builder) policyFacts(items []*domain.WorkItem) policyFacts {
	// The policy files are read by workflow.Load (plain reads, no lock); name
	// them, because their problems change the readiness verdict.
	if names, err := b.rd.list("workflows", ".md"); err != nil {
		b.problem(err)
	} else {
		for _, name := range names {
			b.rd.noteDevsys("workflows/" + name)
		}
	}
	results := workflow.Load(b.root)
	facts := policyFacts{parsed: map[string]*workflow.Policy{}}
	present := map[string]bool{}
	for _, res := range results {
		name := res.File
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		present[strings.TrimSuffix(name, ".md")] = true
		for _, issue := range res.Issues {
			if issue.Severity == workflow.SeverityError {
				facts.problems = append(facts.problems, issue.String())
			}
		}
		if !strings.HasSuffix(res.File, ".md") {
			continue // the workflows/ directory itself, or a non-policy entry
		}
		id := strings.TrimSuffix(name, ".md")
		facts.ids = append(facts.ids, id)
		facts.parsed[id] = res.Policy
	}
	sort.Strings(facts.ids)
	for _, wi := range items {
		if wi.Workflow == nil || wi.Workflow.ID == "" || present[wi.Workflow.ID] {
			continue
		}
		facts.problems = append(facts.problems, fmt.Sprintf("work item %s references missing policy %q", wi.ID, wi.Workflow.ID))
	}
	if id := b.defaultPolicy(); id != "" && !present[id] {
		facts.problems = append(facts.problems,
			fmt.Sprintf("config.yaml default_policy %q has no .devsys/workflows/%s.md", id, id))
	}
	sort.Strings(facts.problems)
	return facts
}

// pendingApprovals are the approvals still waiting for a decision; an approval
// whose stage was left is invalidated and no longer waits (方案 §4.9).
func (b *builder) pendingApprovals() []next.PendingApproval {
	names, err := b.rd.list("approvals", ".yaml")
	if err != nil {
		b.problem(err)
		return nil
	}
	var out []next.PendingApproval
	for _, name := range names {
		var a domain.Approval
		if _, err := b.rd.read("approvals/"+name, &a); err != nil {
			b.problem(err)
			continue
		}
		if a.Status != approval.StatusPending || a.InvalidatedAt != nil {
			continue
		}
		out = append(out, next.PendingApproval{ID: a.ID, WorkitemID: a.WorkItemID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// runs fills the execution timeline, newest first, capped at the limit.
func (b *builder) runs() {
	mark := b.rd.mark()
	r := &b.m.Runs
	names, err := b.rd.list("runs", ".yaml")
	if err != nil {
		b.problem(err)
	}
	var runs []*domain.Run
	for _, name := range names {
		var run domain.Run
		if _, err := b.rd.read("runs/"+name, &run); err != nil {
			b.problem(err)
			continue
		}
		runs = append(runs, &run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].StartedAt.After(runs[j].StartedAt)
		}
		return runs[i].ID < runs[j].ID
	})
	if len(runs) > b.limit {
		runs = runs[:b.limit]
		r.Truncated = true
	}
	for _, run := range runs {
		r.Entries = append(r.Entries, b.runEntry(run))
	}
	r.Sources = b.rd.since(mark)
}

func (b *builder) runEntry(run *domain.Run) RunEntry {
	e := RunEntry{
		ID:         run.ID,
		WorkitemID: run.WorkItemID,
		WorkflowID: run.WorkflowID,
		Status:     run.Status,
		Phase:      run.Phase,
		Harness:    run.Agent.Harness,
		Model:      run.Agent.Model,
		AgentID:    run.Agent.ID,
		Workspace:  run.Workspace.Path,
		Branch:     run.Workspace.Branch,
		Attempt:    run.Attempt,
		RetryCount: run.RetryCount,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Advanced:   run.Verification.Advanced,
		Errors:     append([]string{}, run.Result.Errors...),
		StreamTorn: b.streamTorn(run.ID),
	}
	if run.Result.Summary != nil {
		e.Summary = *run.Result.Summary
	}
	if len(e.Errors) == 0 {
		e.Errors = nil
	}
	return e
}

// streamTorn reports whether the run's event stream ends mid-record. It is
// reported, never repaired: repair belongs to the run's writer (M6.1). The
// stream file joins the section's sources whenever it exists.
func (b *builder) streamTorn(id string) bool {
	rel := "runs/" + id + ".jsonl"
	if !b.rd.exists(rel) {
		return false
	}
	b.rd.noteDevsys(rel)
	status, err := storage.InspectJSONL(filepath.Join(b.rd.devsys, filepath.FromSlash(rel)))
	return err == nil && !status.Complete
}

// attemptSignals is the same classification `devsys next` uses, so the view's
// readiness section does not disagree with the CLI.
func (b *builder) attemptSignals(items []*domain.WorkItem) (dead, inflight []next.AttemptRef) {
	names, err := b.rd.list("runs", ".yaml")
	if err != nil {
		return nil, nil
	}
	latest := map[string]*domain.Run{}
	for _, name := range names {
		var run domain.Run
		if _, err := b.rd.read("runs/"+name, &run); err != nil {
			continue
		}
		cur := latest[run.WorkItemID]
		if cur == nil || run.StartedAt.After(cur.StartedAt) || (run.StartedAt.Equal(cur.StartedAt) && run.ID > cur.ID) {
			latest[run.WorkItemID] = &run
		}
	}
	streamLines := map[string]int{}
	for _, r := range latest {
		streamLines[r.ID] = b.streamLineCount(r.ID)
	}
	return next.ClassifyAttempts(b.now, items, latest, streamLines)
}

func (b *builder) streamLineCount(id string) int {
	rel := "runs/" + id + ".jsonl"
	if !b.rd.exists(rel) {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(b.rd.devsys, filepath.FromSlash(rel)))
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range data {
		if c == '\n' {
			n++
		}
	}
	return n
}

func (b *builder) records() {
	mark := b.rd.mark()
	rec := &b.m.Records
	truncated := false

	var decisions []*domain.Decision
	names, err := b.rd.list("decisions", ".yaml")
	if err != nil {
		b.problem(err)
	}
	for _, name := range names {
		var d domain.Decision
		if _, err := b.rd.read("decisions/"+name, &d); err != nil {
			b.problem(err)
			continue
		}
		decisions = append(decisions, &d)
	}
	sort.Slice(decisions, func(i, j int) bool {
		if !decisions[i].CreatedAt.Equal(decisions[j].CreatedAt) {
			return decisions[i].CreatedAt.After(decisions[j].CreatedAt)
		}
		return decisions[i].ID < decisions[j].ID
	})
	decisions, capped := capList(decisions, b.limit)
	truncated = truncated || capped
	for _, d := range decisions {
		created := d.CreatedAt
		rec.Decisions = append(rec.Decisions, RecordRef{
			ID: d.ID, Title: d.Title, Status: d.Status,
			RelatedWorkItems: append([]string{}, d.RelatedWorkItems...),
			CreatedAt:        &created,
		})
	}

	names, err = b.rd.list("findings", ".yaml")
	if err != nil {
		b.problem(err)
	}
	var findings []*domain.Finding
	for _, name := range names {
		var f domain.Finding
		if _, err := b.rd.read("findings/"+name, &f); err != nil {
			b.problem(err)
			continue
		}
		findings = append(findings, &f)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	findings, capped = capList(findings, b.limit)
	truncated = truncated || capped
	for _, f := range findings {
		rec.Findings = append(rec.Findings, RecordRef{
			ID: f.ID, Title: f.Title, Type: f.Type, Status: f.Status, Severity: f.Severity,
			RelatedWorkItems: append([]string{}, f.RelatedWorkItems...),
		})
	}

	names, err = b.rd.list("artifacts", ".yaml")
	if err != nil {
		b.problem(err)
	}
	var artifacts []*domain.Artifact
	for _, name := range names {
		var a domain.Artifact
		if _, err := b.rd.read("artifacts/"+name, &a); err != nil {
			b.problem(err)
			continue
		}
		artifacts = append(artifacts, &a)
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if !artifacts[i].CreatedAt.Equal(artifacts[j].CreatedAt) {
			return artifacts[i].CreatedAt.After(artifacts[j].CreatedAt)
		}
		return artifacts[i].ID < artifacts[j].ID
	})
	artifacts, capped = capList(artifacts, b.limit)
	truncated = truncated || capped
	for _, a := range artifacts {
		created := a.CreatedAt
		rec.Artifacts = append(rec.Artifacts, RecordRef{
			ID: a.ID, Title: a.Name, Type: a.Type, Status: a.Status, Version: a.Version,
			RelatedWorkItems: append([]string{}, a.RelatedWorkItems...),
			CreatedAt:        &created,
		})
	}
	rec.Truncated = truncated
	rec.Sources = b.rd.since(mark)
}

// knowledge fills the page layer's index and freshness (M5.3). While pages
// exist the status mirrors `devsys knowledge status`; without a page layer it
// is the documented degradation, and an unanswerable freshness question is
// "unavailable", never "fresh" (方案 §12.5).
func (b *builder) knowledge(items []*domain.WorkItem) {
	mark := b.rd.mark()
	k := &b.m.Knowledge
	var failed []string
	k.Generator = b.generator()
	if snapshot, err := knowledge.LoadSnapshot(b.root); err == nil {
		k.IndexReady = true
		k.IndexFiles = snapshot.Stats.TotalFiles
		b.rd.noteDevsys("knowledge/snapshot.json")
	} else if !os.IsNotExist(err) {
		b.problem(err)
		failed = append(failed, err.Error())
	}
	roots := b.pageRoots()
	for _, root := range roots {
		b.rd.noteRelDir(root)
	}
	report, err := knowledge.Scan(b.root, roots)
	if err != nil {
		b.problem(err)
		failed = append(failed, err.Error())
	}
	for _, problem := range report.Problems {
		b.problemf("%s", problem.String())
	}
	if report.Errors() > 0 {
		failed = append(failed, fmt.Sprintf("the page layer has %d invalid page(s)", report.Errors()))
	}
	state, adapter, err := b.knowledgeState()
	k.Adapter = adapter
	if err != nil {
		b.problem(err)
		failed = append(failed, err.Error())
	} else if state != nil {
		k.Baseline = state.Baseline.Commit
	}
	k.Head, k.Branch = b.m.Baseline.Commit, b.m.Baseline.Branch

	switch pages := report.Pages; {
	case len(pages) == 0:
		k.Status = KnowledgeMissing
		k.Reason = "无知识页面层：未安装或未配置生成器（方案 §12.6）"
	default:
		freshness, err := knowledge.Evaluate(b.root, pages, state)
		if err != nil {
			// The freshness question cannot be answered (no git, for
			// example). Saying "fresh" here would be a claim we cannot back.
			k.Status = KnowledgeUnavailable
			k.Reason = "freshness unavailable: " + err.Error()
			k.Degraded = true
			break
		}
		k.ChangedFiles = freshness.ChangedFiles
		k.Affected = append(k.Affected, freshness.Affected()...)
		k.Unverifiable = append(k.Unverifiable, freshness.Unverifiable()...)
		sort.Strings(k.Affected)
		sort.Strings(k.Unverifiable)
		verdicts := make(map[string]knowledge.PageFreshness, len(freshness.Pages))
		for _, page := range freshness.Pages {
			verdicts[page.Path] = page
		}
		items, texts := relatedTexts(items)
		kept := pages
		if len(kept) > b.limit {
			kept = kept[:b.limit]
			k.Truncated = true
		}
		for _, page := range kept {
			verdict := verdicts[page.Path]
			k.Pages = append(k.Pages, PageEntry{
				Path:             page.Path,
				Type:             page.Type,
				Status:           page.Status,
				SourceCommit:     page.SourceCommit,
				Stale:            verdict.Stale,
				Reason:           verdict.Reason,
				RelatedWorkItems: relatedItems(page.Triggers, items, texts),
			})
		}
		k.Status = KnowledgeFresh
		switch {
		case len(k.Affected) > 0 && len(k.Unverifiable) > 0:
			k.Status = KnowledgeStale
			k.Reason = fmt.Sprintf("%d page(s) affected by %d changed file(s); %d page(s) have no sources or baseline and cannot be called fresh",
				len(k.Affected), k.ChangedFiles, len(k.Unverifiable))
		case len(k.Affected) > 0:
			k.Status = KnowledgeStale
			k.Reason = fmt.Sprintf("%d page(s) affected by %d changed file(s)", len(k.Affected), k.ChangedFiles)
		case len(k.Unverifiable) > 0:
			k.Status = KnowledgeStale
			k.Reason = fmt.Sprintf("%d page(s) have no sources or baseline and cannot be called fresh", len(k.Unverifiable))
		default:
			k.Reason = fmt.Sprintf("all %d page(s) match the baseline %s", len(pages), shortCommit(k.Baseline))
		}
	}
	if len(failed) > 0 {
		k.Degraded = true
		k.Reason = strings.TrimSpace(k.Reason + " " + strings.Join(failed, "; "))
	}
	k.Sources = b.rd.since(mark)
}

// knowledgeState loads the page layer's own state and merges the configured
// generator's imported state under it, the composition `devsys knowledge
// status` performs (M5.6, 方案 §12.6). A missing local state is not an error.
func (b *builder) knowledgeState() (*knowledge.State, string, error) {
	local, err := knowledge.LoadState(b.root)
	if err != nil && !knowledge.MissingState(err) {
		return nil, "", fmt.Errorf("knowledge state: %w", err)
	}
	if err == nil {
		b.rd.noteDevsys("knowledge/state.json")
	}
	adapter, ok := knowledge.AdapterByName(b.generator())
	if !ok {
		return local, "", nil
	}
	imported, err := adapter.Import(b.root)
	if err != nil {
		return nil, "", fmt.Errorf("knowledge import (%s): %w", adapter.Name(), err)
	}
	return knowledge.MergeStates(local, imported), adapter.Name(), nil
}

func (b *builder) generator() string {
	if b.md != nil && b.md.Config != nil {
		return strings.TrimSpace(b.md.Config.KnowledgeGenerator)
	}
	return ""
}

// pageRoots resolves the page roots the way the knowledge commands do: the
// project's configuration wins, the built-in candidates are the fallback.
func (b *builder) pageRoots() []string {
	if b.md != nil && b.md.Config != nil && len(b.md.Config.KnowledgePages) > 0 {
		return b.md.Config.KnowledgePages
	}
	return knowledge.ExistingRoots(b.root, knowledge.DefaultRoots)
}

// relatedTexts precomputes the trigger text of every work item once.
func relatedTexts(items []*domain.WorkItem) ([]*domain.WorkItem, []string) {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = knowledge.WorkItemText(item)
	}
	return items, texts
}

// relatedItems lists the work items a page is relevant to, using the trigger
// rule the context assembly uses (M5.6). The sources rule needs the caller's
// paths, so it stays with `devsys context get --task`.
func relatedItems(triggers []string, items []*domain.WorkItem, texts []string) []string {
	var out []string
	for i, item := range items {
		if _, ok := knowledge.TriggerHit(triggers, texts[i]); ok {
			out = append(out, item.ID)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// readBaseline reads the git state with read-only commands. A repository that
// cannot answer reports Available=false with the reason; the view never fails
// over git, and Dirty stays null rather than claiming a clean tree.
func readBaseline(root string) Baseline {
	commit, branch, err := knowledge.Head(root)
	if err != nil {
		return Baseline{Available: false, Reason: err.Error()}
	}
	b := Baseline{Available: true, Commit: commit, Branch: branch}
	changed, err := knowledge.Changes(root, "HEAD")
	if err != nil {
		b.Reason = "working tree state unavailable: " + err.Error()
		return b
	}
	dirty := len(changed) > 0
	b.Dirty = &dirty
	b.ChangedFiles = len(changed)
	return b
}

// collectSources is the sorted union of every section's sources.
func collectSources(m *Model) []string {
	all := make([]string, 0, 16)
	all = append(all, m.Project.Sources...)
	all = append(all, m.Progress.Sources...)
	all = append(all, m.Runs.Sources...)
	all = append(all, m.Records.Sources...)
	all = append(all, m.Knowledge.Sources...)
	return sortedUnique(all)
}

// pendingIDs names the pending transactions.
func pendingIDs(txns []storage.PendingTxn) []string {
	out := make([]string, 0, len(txns))
	for _, txn := range txns {
		out = append(out, txn.ID)
	}
	sort.Strings(out)
	return out
}

// capList caps a sorted list at limit and reports whether it was truncated.
func capList[T any](list []T, limit int) ([]T, bool) {
	if len(list) <= limit {
		return list, false
	}
	return list[:limit], true
}

// itemFrom projects a work item onto the view's Item.
func itemFrom(wi *domain.WorkItem) Item {
	it := Item{
		ID:              wi.ID,
		Title:           wi.Title,
		Type:            wi.Type,
		Status:          wi.Status,
		Priority:        wi.Priority,
		Dependencies:    append([]string{}, wi.Dependencies...),
		SchedulingState: wi.SchedulingState,
		LeaseOwner:      wi.LeaseOwner,
		LeaseUntil:      wi.LeaseUntil,
		NextAttemptAt:   wi.NextAttemptAt,
		ActiveRunID:     wi.ActiveRunID,
		PreviousStatus:  wi.PreviousStatus,
		CreatedAt:       wi.CreatedAt,
		UpdatedAt:       wi.UpdatedAt,
	}
	if len(it.Dependencies) == 0 {
		it.Dependencies = nil
	}
	if wi.ParentID != nil {
		it.ParentID = *wi.ParentID
	}
	if wi.Workflow != nil {
		it.Workflow, it.WorkflowStep, it.WorkflowPaused = wi.Workflow.ID, wi.Workflow.Step, wi.Workflow.Paused
	}
	if wi.AssignedAgent != nil {
		it.AssignedAgent = *wi.AssignedAgent
	}
	if wi.AssignedHarness != nil {
		it.AssignedHarness = *wi.AssignedHarness
	}
	return it
}

// timePtr returns a pointer for a non-zero time, nil otherwise.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// shortCommit abbreviates a commit for a human-readable reason.
func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "(none)"
	}
	return commit
}
