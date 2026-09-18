package app

import (
	"context"
	"sort"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/record"
)

// ContextView is the working context an agent needs to orient without
// reading source: project facts, progress, the recommended action and the
// most recent records (as references — read the bodies on demand).
type ContextView struct {
	Project         *domain.Project     `json:"project"`
	State           domain.CurrentState `json:"state"`
	Counts          map[string]int      `json:"counts"`
	Verdict         string              `json:"verdict"`
	Next            any                 `json:"next"`
	Risks           any                 `json:"risks,omitempty"`
	RecentDecisions []RecordRef         `json:"recent_decisions"`
	RecentFindings  []RecordRef         `json:"recent_findings"`
	RecentArtifacts []RecordRef         `json:"recent_artifacts"`
	RecentEvents    []EventRef          `json:"recent_events"`
	Notices         []string            `json:"notices,omitempty"`
}

// RecordRef is a pointer to a record: identity, title, status and when it
// changed. The body stays one read away (`decision_get`, `finding_get`, …).
type RecordRef struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// EventRef is a pointer to one event.
type EventRef struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	Subject string    `json:"subject"`
	Actor   string    `json:"actor"`
	Time    time.Time `json:"time"`
}

// WorkitemContextView is the context assembled around one work item.
type WorkitemContextView struct {
	WorkItem  *domain.WorkItem `json:"workitem"`
	Version   string           `json:"version"`
	Workflow  *WorkflowView    `json:"workflow,omitempty"`
	Decisions []RecordRef      `json:"decisions"`
	Findings  []RecordRef      `json:"findings"`
	Artifacts []RecordRef      `json:"artifacts"`
	Comments  []EventRef       `json:"comments"`
	Notices   []string         `json:"notices,omitempty"`
}

// ContextGet assembles the project working context. limit bounds each
// reference list (0 selects the default of 5).
func (s *Service) ContextGet(ctx context.Context, limit int) (ContextView, error) {
	return s.context(ctx, limit, false)
}

// ContextRefresh is ContextGet with the policy and readiness notices spelled
// out: the caller asked for a fresh evaluation, so fallbacks are surfaced
// rather than summarized away.
func (s *Service) ContextRefresh(ctx context.Context, limit int) (ContextView, error) {
	return s.context(ctx, limit, true)
}

// ContextCompact returns the smallest useful context: project identity,
// progress counts and the recommended action, without record references.
func (s *Service) ContextCompact(ctx context.Context) (ContextView, error) {
	view, err := s.context(ctx, 0, false)
	if err != nil {
		return ContextView{}, err
	}
	view.RecentDecisions = nil
	view.RecentFindings = nil
	view.RecentArtifacts = nil
	view.RecentEvents = nil
	return view, nil
}

func (s *Service) context(ctx context.Context, limit int, withNotices bool) (ContextView, error) {
	if limit <= 0 {
		limit = 5
	}
	md, err := s.project()
	if err != nil {
		return ContextView{}, err
	}
	report, items, err := s.Next(ctx)
	if err != nil {
		return ContextView{}, err
	}
	view := ContextView{
		Project: md.Project,
		State:   md.Project.CurrentState,
		Verdict: report.Verdict,
		Next:    report.Next,
		Risks:   report.Risks,
	}
	if items == nil {
		// The inspection is not trustworthy (pending transactions or no lock
		// ever written): counts stay absent and the reason is spelled out —
		// an empty map would read as "no work items", a fabricated fact.
		view.Notices = append(view.Notices, "work item counts unavailable: readiness inspection is limited (see risks)")
	} else {
		view.Counts = map[string]int{}
		for _, wi := range items {
			view.Counts[wi.Status]++
		}
	}

	decisions, err := recordRefs(s, ctx, record.KindDecision, limit)
	if err != nil {
		return ContextView{}, err
	}
	view.RecentDecisions = decisions
	findings, err := recordRefs(s, ctx, record.KindFinding, limit)
	if err != nil {
		return ContextView{}, err
	}
	view.RecentFindings = findings
	artifacts, err := recordRefs(s, ctx, record.KindArtifact, limit)
	if err != nil {
		return ContextView{}, err
	}
	view.RecentArtifacts = artifacts

	recent, err := events.New(s.Root).Read(ctx, events.Filter{})
	if err != nil {
		return ContextView{}, s.storeError(err)
	}
	view.RecentEvents = recentEvents(recent, limit)

	if withNotices {
		for _, p := range s.policyProblems(ctx) {
			view.Notices = append(view.Notices, p)
		}
	}
	return view, nil
}

// ContextForWorkitem assembles everything an executor needs about one work
// item: the item itself, its workflow candidates, and the records that
// reference it.
func (s *Service) ContextForWorkitem(ctx context.Context, id string) (WorkitemContextView, error) {
	view, err := s.WorkitemGet(ctx, id)
	if err != nil {
		return WorkitemContextView{}, err
	}
	out := WorkitemContextView{WorkItem: view.Item, Version: view.Version}

	if view.Item.Workflow != nil {
		wf, err := s.WorkflowGet(ctx, id)
		switch {
		case err == nil:
			out.Workflow = &wf
			if wf.Notice != "" {
				out.Notices = append(out.Notices, wf.Notice)
			}
		default:
			// A failed evaluation is a fact the caller must see: silently
			// omitting the workflow would read as "no workflow".
			out.Notices = append(out.Notices, "workflow context unavailable: "+err.Error())
		}
	}

	decisions, err := record.New(s.Root).ListDecisions(ctx)
	if err != nil {
		return WorkitemContextView{}, s.storeError(err)
	}
	for _, d := range decisions {
		if references(d.RelatedWorkItems, id) {
			out.Decisions = append(out.Decisions, RecordRef{Ref: "decision://" + d.ID, Title: d.Title, Status: d.Status})
		}
	}
	findings, err := record.New(s.Root).ListFindings(ctx)
	if err != nil {
		return WorkitemContextView{}, s.storeError(err)
	}
	for _, f := range findings {
		if references(f.RelatedWorkItems, id) {
			out.Findings = append(out.Findings, RecordRef{Ref: "finding://" + f.ID, Title: f.Title, Status: f.Status})
		}
	}
	artifacts, err := record.New(s.Root).ListArtifacts(ctx)
	if err != nil {
		return WorkitemContextView{}, s.storeError(err)
	}
	for _, a := range artifacts {
		if references(a.RelatedWorkItems, id) {
			out.Artifacts = append(out.Artifacts, RecordRef{Ref: "artifact://" + a.ID, Title: a.Name, Status: a.Status})
		}
	}
	comments, err := events.New(s.Root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: id},
		Type:    "comment",
	})
	if err != nil {
		return WorkitemContextView{}, s.storeError(err)
	}
	for _, ev := range comments {
		out.Comments = append(out.Comments, EventRef{ID: ev.ID, Type: ev.Type, Subject: ev.Subject.Type + ":" + ev.Subject.ID, Actor: ev.Actor, Time: ev.Time})
	}
	return out, nil
}

// policyProblems lists located policy failures (read-only).
func (s *Service) policyProblems(ctx context.Context) []string {
	summaries, warnings, err := s.WorkflowList(ctx)
	_ = summaries
	var out []string
	if err != nil {
		var ae *Error
		if asAppError(err, &ae) {
			for _, p := range ae.Problems {
				out = append(out, p.String())
			}
		}
		return out
	}
	for _, w := range warnings {
		out = append(out, w.String())
	}
	return out
}

func asAppError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}

// recentRecords maps the newest N records through fn (records are listed in
// ascending ID order).
func recentRecords(count, limit int, fn func(i int) RecordRef) []RecordRef {
	if count == 0 {
		return []RecordRef{}
	}
	if limit > count {
		limit = count
	}
	out := make([]RecordRef, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, fn(i))
	}
	return out
}

// recentEvents keeps the newest N events (the stream is ascending by time).
func recentEvents(all []*domain.Event, limit int) []EventRef {
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	out := make([]EventRef, 0, len(all))
	for _, ev := range all {
		out = append(out, EventRef{ID: ev.ID, Type: ev.Type, Subject: ev.Subject.Type + ":" + ev.Subject.ID, Actor: ev.Actor, Time: ev.Time})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func references(list []string, id string) bool {
	for _, item := range list {
		if item == id {
			return true
		}
	}
	return false
}
