package app

import (
	"context"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

// RunView is one run record plus its version hash.
type RunView struct {
	Run     *domain.Run `json:"run"`
	Version string      `json:"version"`
}

// RunListRequest filters the run listing.
type RunListRequest struct {
	WorkItemID string
	Status     string
}

// RunList lists runs, optionally filtered by work item or status.
func (s *Service) RunList(ctx context.Context, req RunListRequest) ([]*domain.Run, error) {
	items, err := run.New(s.Root).List(ctx)
	if err != nil {
		return nil, s.storeError(err)
	}
	out := make([]*domain.Run, 0, len(items))
	for _, r := range items {
		if req.WorkItemID != "" && r.WorkItemID != req.WorkItemID {
			continue
		}
		if req.Status != "" && r.Status != req.Status {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// RunGet reads one run with its version hash.
func (s *Service) RunGet(ctx context.Context, id string) (RunView, error) {
	r, raw, err := readRun(ctx, s, id)
	if err != nil {
		return RunView{}, err
	}
	return RunView{Run: r, Version: versionHash(raw)}, nil
}

// readRun reads a run record together with its raw bytes (one atomic read,
// so the reported version and the update guard cannot tear apart).
func readRun(ctx context.Context, s *Service, id string) (*domain.Run, []byte, error) {
	r, raw, err := run.New(s.Root).ReadSnapshot(ctx, id)
	if err != nil {
		return nil, nil, s.storeError(err)
	}
	return r, raw, nil
}

// CreateRunRequest is the input to RunCreate.
type CreateRunRequest struct {
	WorkItemID string
	WorkflowID string
	Phase      string
	Actor      string
	Reason     string
	Agent      domain.RunAgent
	Workspace  domain.Workspace
}

// RunCreate records a standalone run for a work item. Claim-created runs (the
// normal path) carry lease credentials; a standalone run carries none, so it
// must not be treated as an active claim.
func (s *Service) RunCreate(ctx context.Context, req CreateRunRequest) (RunView, error) {
	if req.WorkItemID == "" || req.Actor == "" || req.Reason == "" {
		return RunView{}, Usagef("run create requires workitem, actor and reason")
	}
	wi, err := s.WorkitemGet(ctx, req.WorkItemID)
	if err != nil {
		return RunView{}, err
	}
	now := s.now()
	workflowID := req.WorkflowID
	if workflowID == "" && wi.Item.Workflow != nil {
		workflowID = wi.Item.Workflow.ID
	}
	r := &domain.Run{
		ProjectID:  wi.Item.ProjectID,
		WorkItemID: wi.Item.ID,
		WorkflowID: workflowID,
		Agent:      req.Agent,
		Workspace:  req.Workspace,
		Status:     "created",
		Attempt:    1,
		Phase:      req.Phase,
		StartedAt:  now,
	}
	id, err := run.New(s.Root).Create(ctx, r)
	if err != nil {
		return RunView{}, s.storeError(err)
	}
	// Provenance belongs in the append-only stream: the run record itself
	// carries the execution evidence, not who asked for it or why.
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "run_created",
		Subject:   domain.Reference{Type: "run", ID: id},
		ProjectID: wi.Item.ProjectID,
		Actor:     req.Actor,
		Content:   req.Reason,
		Time:      now,
	}); err != nil {
		return RunView{}, s.storeError(err)
	}
	return s.RunGet(ctx, id)
}

// UpdateRunRequest patches a run under the version guard. Add* fields append.
type UpdateRunRequest struct {
	ID           string
	Expect       string
	Status       *string
	Phase        *string
	AddLogs      []string
	AddCommands  []string
	AddTests     []string
	ChangedFiles []string
	Result       *domain.RunResult
}

// RunUpdate applies a run patch (evidence accumulation; lifecycle authority
// stays with the executor paths).
func (s *Service) RunUpdate(ctx context.Context, req UpdateRunRequest) (RunView, error) {
	if req.Status == nil && req.Phase == nil && len(req.AddLogs) == 0 && len(req.AddCommands) == 0 &&
		len(req.AddTests) == 0 && req.ChangedFiles == nil && req.Result == nil {
		return RunView{}, Usagef("run update requires at least one field to change")
	}
	r, raw, err := readRun(ctx, s, req.ID)
	if err != nil {
		return RunView{}, err
	}
	if err := checkExpect(req.Expect, raw, "run"); err != nil {
		return RunView{}, err
	}
	if req.Status != nil {
		r.Status = *req.Status
		if *req.Status == "completed" || *req.Status == "failed" || *req.Status == "cancelled" {
			finished := s.now()
			r.FinishedAt = &finished
		}
	}
	if req.Phase != nil {
		r.Phase = *req.Phase
	}
	r.Logs = append(r.Logs, req.AddLogs...)
	r.Commands = append(r.Commands, req.AddCommands...)
	r.Tests = append(r.Tests, req.AddTests...)
	if req.ChangedFiles != nil {
		r.ChangedFiles = req.ChangedFiles
	}
	if req.Result != nil {
		r.Result = *req.Result
	}
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return RunView{}, s.storeError(err)
	}
	return s.RunGet(ctx, req.ID)
}

// RunLogView is the evidence trail a run accumulated.
type RunLogView struct {
	RunID        string           `json:"run_id"`
	Status       string           `json:"status"`
	Phase        string           `json:"phase"`
	Logs         []string         `json:"logs"`
	Commands     []string         `json:"commands"`
	Tests        []string         `json:"tests"`
	ChangedFiles []string         `json:"changed_files"`
	Result       domain.RunResult `json:"result"`
	Events       []*domain.Event  `json:"events,omitempty"`
}

// RunLog returns the run's own evidence plus the events recorded against it.
func (s *Service) RunLog(ctx context.Context, id string, limit int) (RunLogView, error) {
	r, err := run.New(s.Root).Get(ctx, id)
	if err != nil {
		return RunLogView{}, s.storeError(err)
	}
	view := RunLogView{
		RunID: r.ID, Status: r.Status, Phase: r.Phase,
		Logs: r.Logs, Commands: r.Commands, Tests: r.Tests,
		ChangedFiles: r.ChangedFiles, Result: r.Result,
	}
	if view.Logs == nil {
		view.Logs = []string{}
	}
	related, err := events.New(s.Root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "run", ID: id},
	})
	if err != nil {
		return RunLogView{}, s.storeError(err)
	}
	if limit > 0 && len(related) > limit {
		related = related[len(related)-limit:]
	}
	view.Events = related
	return view, nil
}

// HeartbeatView reports the lease the heartbeat extended.
type HeartbeatView struct {
	WorkitemID  string    `json:"workitem_id"`
	RunID       string    `json:"run_id"`
	LeaseUntil  time.Time `json:"lease_until"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
}

// RunHeartbeat extends the lease of the run's work item (owner and token must
// match the active lease — the same contract claim/release use).
func (s *Service) RunHeartbeat(ctx context.Context, runID, owner, token, actor, reason string, extendBy time.Duration) (HeartbeatView, error) {
	if owner == "" || token == "" || actor == "" || reason == "" {
		return HeartbeatView{}, Usagef("run heartbeat requires owner, token, actor and reason")
	}
	r, err := run.New(s.Root).Get(ctx, runID)
	if err != nil {
		return HeartbeatView{}, s.storeError(err)
	}
	wi, raw, err := s.readSnapshot(ctx, r.WorkItemID, "")
	if err != nil {
		return HeartbeatView{}, err
	}
	if err := s.items().Heartbeat(ctx, wi.ID, workitem.HeartbeatOptions{
		Owner: owner, Token: token, Expected: raw, ExtendBy: extendBy,
		Actor: actor, Reason: reason, Now: s.now(),
	}); err != nil {
		return HeartbeatView{}, s.storeError(err)
	}
	lease, err := s.items().LeaseInspection(ctx, wi.ID)
	if err != nil {
		return HeartbeatView{}, s.storeError(err)
	}
	return HeartbeatView{WorkitemID: wi.ID, RunID: runID, LeaseUntil: lease.LeaseUntil, HeartbeatAt: lease.HeartbeatAt}, nil
}
