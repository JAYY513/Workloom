package app

import (
	"context"
	"sort"
	"time"

	"workloom/internal/domain"
	"workloom/internal/next"
	"workloom/internal/record"
)

// SessionRequest is the input to SessionStart (方案 §8.3): who is starting,
// where, and with what intent.
type SessionRequest struct {
	Harness   string
	AgentID   string
	Workspace string
	Intent    string
	// Compact trims the context payload for small windows: fewer references
	// and no per-item detail beyond the essentials.
	Compact bool
}

// SessionView is the one-shot orientation a new session needs: project facts,
// current state, work in flight, the single recommended next action, and the
// context behind it. One call, no follow-up queries required.
type SessionView struct {
	Session               SessionIdentity     `json:"session"`
	Project               SessionProject      `json:"project"`
	State                 domain.CurrentState `json:"state"`
	CurrentWorkitems      []SessionWorkitem   `json:"current_workitems"`
	RecommendedNextAction SessionAction       `json:"recommended_next_action"`
	Context               SessionContext      `json:"context"`
	Notices               []string            `json:"notices,omitempty"`
}

// SessionIdentity echoes what the caller declared. It is never persisted and
// does not steer the recommendation (a session start must not change state);
// M6 may use the workspace when it scopes runs.
type SessionIdentity struct {
	Harness   string `json:"harness,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Intent    string `json:"intent,omitempty"`
}

// SessionProject is the project identity a session reports.
type SessionProject struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CurrentPhase string `json:"current_phase"`
}

// SessionWorkitem is one in-flight work item.
type SessionWorkitem struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Priority  int       `json:"priority"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionAction is the recommended next action, with the command that
// performs it.
type SessionAction struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Reason  string `json:"reason"`
	Command string `json:"command,omitempty"`
}

// SessionContext is the background a session should have: the project
// summary plus references to the decisions, findings and artifacts that
// matter. Bodies stay one read away.
type SessionContext struct {
	ProjectSummary    string      `json:"project_summary"`
	RelevantDecisions []RecordRef `json:"relevant_decisions"`
	RecentFindings    []RecordRef `json:"recent_findings"`
	RelevantArtifacts []RecordRef `json:"relevant_artifacts"`
	// RecentEvents is omitted in compact mode by design (the key is absent
	// rather than empty).
	RecentEvents []EventRef `json:"recent_events,omitempty"`
}

// SessionStart assembles the first call of a session (方案 §8.3, 实施计划
// M4.4). It is read-only: the harness's own agent identity is reported back,
// never recorded, so starting a session cannot change project state.
func (s *Service) SessionStart(ctx context.Context, req SessionRequest) (SessionView, error) {
	md, err := s.project()
	if err != nil {
		return SessionView{}, err
	}
	report, items, err := s.Next(ctx)
	if err != nil {
		return SessionView{}, err
	}
	limit := 5
	if req.Compact {
		limit = 3
	}

	view := SessionView{
		Session: SessionIdentity{
			Harness: req.Harness, AgentID: req.AgentID, Workspace: req.Workspace, Intent: req.Intent,
		},
		Project: SessionProject{
			ID:           md.Project.ID,
			Name:         md.Project.Name,
			CurrentPhase: md.Project.CurrentPhase,
		},
		State: md.Project.CurrentState,
	}
	view.CurrentWorkitems = []SessionWorkitem{}
	if items == nil {
		view.Notices = append(view.Notices, "work item state unavailable: readiness inspection is limited (see the readiness risks)")
	} else {
		view.CurrentWorkitems = inFlight(items, limit)
	}

	view.RecommendedNextAction = sessionAction(report.Next)

	decisions, err := recordRefs(s, ctx, record.KindDecision, limit)
	if err != nil {
		return SessionView{}, err
	}
	findings, err := recordRefs(s, ctx, record.KindFinding, limit)
	if err != nil {
		return SessionView{}, err
	}
	artifacts, err := recordRefs(s, ctx, record.KindArtifact, limit)
	if err != nil {
		return SessionView{}, err
	}
	view.Context = SessionContext{
		ProjectSummary:    md.Project.Description,
		RelevantDecisions: decisions,
		RecentFindings:    findings,
		RelevantArtifacts: artifacts,
	}
	if !req.Compact {
		recent, err := s.EventList(ctx, EventListRequest{Limit: limit})
		if err != nil {
			return SessionView{}, err
		}
		for _, ev := range recent {
			view.Context.RecentEvents = append(view.Context.RecentEvents, EventRef{
				ID: ev.ID, Type: ev.Type, Subject: ev.Subject.Type + ":" + ev.Subject.ID, Actor: ev.Actor, Time: ev.Time,
			})
		}
	}
	for _, risk := range report.Risks {
		view.Notices = append(view.Notices, "risk: "+risk.Kind+": "+risk.Detail)
	}
	return view, nil
}

// inFlight lists the work items a session should know about: everything not
// closed, highest priority first (then newest activity), capped at limit.
func inFlight(items []*domain.WorkItem, limit int) []SessionWorkitem {
	out := make([]SessionWorkitem, 0, len(items))
	for _, wi := range items {
		switch wi.Status {
		case domain.StatusReady, domain.StatusInProgress, domain.StatusBlocked,
			domain.StatusReview, domain.StatusVerification:
		default:
			continue
		}
		out = append(out, SessionWorkitem{
			ID: wi.ID, Status: wi.Status, Title: wi.Title, Priority: wi.Priority, UpdatedAt: wi.UpdatedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// sessionAction maps the §7.4 recommendation onto the session response,
// including the command that performs it.
func sessionAction(rec next.Recommendation) SessionAction {
	action := SessionAction{Type: rec.Action, Reason: rec.Reason}
	switch rec.Action {
	case "milestone_review", "report_done":
		action.ID = rec.MilestoneID
	default:
		action.ID = rec.WorkitemID
	}
	// Commands are only emitted when they are actually runnable: a command
	// naming a blank id would be worse than no command at all.
	switch rec.Action {
	case "recover_claim":
		action.Command = `devsys recover --actor operator --reason "recover interrupted state"`
	case "review":
		if rec.WorkitemID != "" {
			action.Command = "devsys workitem get " + rec.WorkitemID
		} else {
			action.Command = "devsys workitem list --status review"
		}
	case "start":
		if rec.WorkitemID != "" {
			action.Command = "devsys workitem claim --id " + rec.WorkitemID + " --owner <owner> --reason <reason>"
		}
	case "start_backlog":
		if rec.WorkitemID != "" {
			action.Command = "devsys workitem transition --id " + rec.WorkitemID + " --to ready --actor <actor> --reason <reason>"
		}
	case "milestone_review", "report_done":
		action.Command = "devsys project status"
	}
	return action
}

// recordRefs lists the newest N references of one record kind.
func recordRefs(s *Service, ctx context.Context, kind record.Kind, limit int) ([]RecordRef, error) {
	store := record.New(s.Root)
	var refs []RecordRef
	switch kind {
	case record.KindDecision:
		items, err := store.ListDecisions(ctx)
		if err != nil {
			return nil, s.storeError(err)
		}
		refs = recentRecords(len(items), limit, func(i int) RecordRef {
			d := items[len(items)-1-i]
			return RecordRef{Ref: "decision://" + d.ID, Title: d.Title, Status: d.Status, UpdatedAt: d.CreatedAt}
		})
	case record.KindFinding:
		items, err := store.ListFindings(ctx)
		if err != nil {
			return nil, s.storeError(err)
		}
		refs = recentRecords(len(items), limit, func(i int) RecordRef {
			f := items[len(items)-1-i]
			return RecordRef{Ref: "finding://" + f.ID, Title: f.Title, Status: f.Status}
		})
	case record.KindArtifact:
		items, err := store.ListArtifacts(ctx)
		if err != nil {
			return nil, s.storeError(err)
		}
		refs = recentRecords(len(items), limit, func(i int) RecordRef {
			a := items[len(items)-1-i]
			return RecordRef{Ref: "artifact://" + a.ID, Title: a.Name, Status: a.Status, UpdatedAt: a.UpdatedAt}
		})
	}
	if refs == nil {
		refs = []RecordRef{}
	}
	return refs, nil
}
