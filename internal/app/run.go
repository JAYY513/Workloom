package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/run"
	"github.com/JAYY513/Workloom/internal/workitem"
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
		if isTerminalRun(*req.Status) {
			// Lifecycle is what the completion check guards; a status patch
			// must not be able to reach around it (方案 §4.8).
			return RunView{}, Usagef("run update cannot set the terminal status %q; use run complete|fail|cancel", *req.Status)
		}
		r.Status = *req.Status
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

// Terminal run statuses are the §4.8 vocabulary: an attempt either succeeded,
// failed, ran out of time, stalled or was canceled. The lifecycle commands
// speak exactly these words; the generic RunUpdate stays free-form for
// evidence accumulation.
const (
	// RunRunning is the status of an attempt that is executing.
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunTimedOut  = "timed_out"
	RunStalled   = "stalled"
	RunCanceled  = "canceled"
)

// isTerminalRun reports whether a run has already ended.
func isTerminalRun(status string) bool {
	switch status {
	case RunSucceeded, RunFailed, RunTimedOut, RunStalled, RunCanceled:
		return true
	default:
		return false
	}
}

// RunFinishRequest records how an attempt ended when the outcome is decided by
// a caller rather than by a command's exit status: an operator, the dispatch
// tick (M6.4) or a harness reporting through the tool surface.
type RunFinishRequest struct {
	RunID   string
	Expect  string
	Outcome string
	Actor   string
	Reason  string
	// Note is optional detail kept with the run's result evidence.
	Note string
	// Force lets a reviewer record an outcome the completion check refused.
	// It requires By: an exception to "no evidence, no verified" has to be
	// traceable to a person (方案 §4.8).
	Force bool
	By    string
}

// RunFinish writes the terminal status, the finish time and a run_finished
// event under the version guard. Ending a run that already ended is refused
// rather than silently repeated, so two actors cannot both claim the outcome.
func (s *Service) RunFinish(ctx context.Context, req RunFinishRequest) (RunView, error) {
	if req.RunID == "" || req.Actor == "" || req.Reason == "" {
		return RunView{}, Usagef("run finish requires id, actor and reason")
	}
	if !isTerminalRun(req.Outcome) {
		return RunView{}, Usagef("unknown run outcome %q (succeeded | failed | timed_out | stalled | canceled)", req.Outcome)
	}
	if req.Force && req.By == "" {
		return RunView{}, Usagef("forcing an outcome requires --by <reviewer>: an exception has to be traceable")
	}
	r, raw, err := readRun(ctx, s, req.RunID)
	if err != nil {
		return RunView{}, err
	}
	if err := checkExpect(req.Expect, raw, "run"); err != nil {
		return RunView{}, err
	}
	if isTerminalRun(r.Status) {
		return RunView{}, Preconditionf("run %s already ended as %s", r.ID, r.Status)
	}
	// Completing an attempt is the one transition that claims success, so it
	// is the one the completion check guards (方案 §4.8).
	var check CompletionCheck
	if req.Outcome == RunSucceeded {
		check = s.verifyCompletion(ctx, r)
		switch {
		case check.Advanced, req.Force, check.Skipped:
			advanced := check.Advanced
			r.Verification.Advanced = &advanced
			r.Verification.HeadSHAAtComplete = check.CurrentHead
			if req.Force {
				// An override is always attributed, even when the branch did
				// advance: the event says who accepted it, so the record must.
				reviewer := req.By
				r.Verification.VerifiedBy = &reviewer
			}
		default:
			reason := check.Reason
			if reason == "" {
				reason = "the completion check found no advance"
			}
			routed, err := s.refuseCompletion(ctx, r, req.Actor, reason, check)
			if err != nil {
				return RunView{}, err
			}
			where := "the work item is in review — review it and pass --force --by <reviewer> to accept, or fix the branch and retry"
			if !routed {
				where = fmt.Sprintf("the work item did not enter review (its review gate is unmet — add a comment first: `workloom workitem comment --id %s --text \"...\" --actor <you>`), then transition or pass --force --by <reviewer> to accept anyway", r.WorkItemID)
			}
			return RunView{}, Preconditionf(
				"run %s cannot be marked succeeded: %s (claim head %s, current head %s); %s",
				r.ID, reason, orNone(check.ClaimHead), orNone(check.CurrentHead), where)
		}
	}
	now := s.now()
	r.Status = req.Outcome
	r.FinishedAt = &now
	if req.Note != "" {
		r.Result.Errors = append(r.Result.Errors, req.Note)
	}
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return RunView{}, s.storeError(err)
	}
	eventType := "run_finished"
	switch {
	case req.Outcome == RunSucceeded && req.Force:
		eventType = "completion_overridden"
	case req.Outcome == RunSucceeded && check.Skipped:
		eventType = "run_finished"
	case req.Outcome == RunSucceeded:
		eventType = "completion_verified"
	}
	content := fmt.Sprintf("%s: %s", req.Outcome, req.Reason)
	if req.Outcome == RunSucceeded {
		verdict := "advanced"
		switch {
		case check.Skipped:
			verdict = "git check not applicable"
		case r.Verification.Advanced != nil && !*r.Verification.Advanced:
			verdict = "not advanced"
		}
		content = fmt.Sprintf("%s (%s, head %s)", content, verdict, orNone(r.Verification.HeadSHAAtComplete))
		if req.Force {
			content += fmt.Sprintf("; accepted by %s", req.By)
		}
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      eventType,
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   content,
		Time:      now,
	}); err != nil {
		return RunView{}, s.storeError(err)
	}
	// Success closes the attempt, and the claim served its purpose: release
	// it like the refusal path does, so `run complete` does not leave the
	// work item fenced behind a lease nobody will present (#342). Only the
	// lease this run bound is touched — a newer attempt's claim is not.
	if req.Outcome == RunSucceeded {
		lease, lerr := s.items().LeaseInspection(ctx, r.WorkItemID)
		switch {
		case lerr != nil && !errors.Is(lerr, workitem.ErrNotClaimed):
			return RunView{}, s.storeError(lerr)
		case lerr == nil && lease.Owner != "" && lease.RunID == r.ID:
			if err := s.items().Release(ctx, r.WorkItemID, workitem.ReleaseOptions{
				ForCompleted: true,
				BoundRunID:   r.ID,
				Actor:        req.Actor,
				Reason:       fmt.Sprintf("run completed: %s", req.Reason),
				Now:          s.now(),
			}); err != nil {
				return RunView{}, s.storeError(err)
			}
		}
	}
	return s.RunGet(ctx, r.ID)
}
