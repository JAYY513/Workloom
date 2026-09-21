package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/JAYY513/Workloom/internal/approval"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
	"github.com/JAYY513/Workloom/internal/workitem"
)

// ApprovalFilter narrows an approval listing.
type ApprovalFilter struct {
	WorkItemID string
	Status     string
}

// ApprovalList lists approvals (newest last, store order).
func (s *Service) ApprovalList(ctx context.Context, filter ApprovalFilter) ([]*domain.Approval, error) {
	approvals, err := approval.New(s.Root).List(ctx, approval.Filter{
		WorkItemID: filter.WorkItemID, Status: filter.Status,
	})
	if err != nil {
		return nil, s.approvalError(err)
	}
	if approvals == nil {
		approvals = []*domain.Approval{}
	}
	return approvals, nil
}

// ApprovalGet reads one approval.
func (s *Service) ApprovalGet(ctx context.Context, id string) (*domain.Approval, error) {
	apr, err := approval.New(s.Root).Get(ctx, id)
	if err != nil {
		return nil, s.approvalError(err)
	}
	return apr, nil
}

// RequestApprovalRequest is the input to ApprovalRequest.
type RequestApprovalRequest struct {
	WorkItemID string
	Stage      string
	Scope      string
	RunID      string
	Actor      string
	Reason     string
}

// ApprovalRequest records a pending approval. The returned warning (if any)
// names a stage-gate request no policy will ever consume.
func (s *Service) ApprovalRequest(ctx context.Context, req RequestApprovalRequest) (*domain.Approval, string, error) {
	if req.WorkItemID == "" || req.Stage == "" || req.Actor == "" || req.Reason == "" {
		return nil, "", Usagef("approval request requires workitem, stage, actor and reason")
	}
	scope := req.Scope
	if scope == "" {
		scope = approval.ScopeStageGate
	}
	switch scope {
	case approval.ScopeStageGate, approval.ScopeAction:
	default:
		return nil, "", Usagef("scope must be %s or %s (got %q)", approval.ScopeStageGate, approval.ScopeAction, scope)
	}
	wi, err := s.WorkitemGet(ctx, req.WorkItemID)
	if err != nil {
		return nil, "", err
	}
	requestedStatus := ""
	if scope == approval.ScopeStageGate {
		requestedStatus = wi.Item.Status
	}
	apr, err := approval.New(s.Root).Request(ctx, approval.RequestOptions{
		Scope: scope, WorkItemID: wi.Item.ID, RunID: req.RunID, Stage: req.Stage,
		RequestedStatus: requestedStatus, ProjectID: wi.Item.ProjectID,
		RequestedBy: req.Actor, Reason: req.Reason, Now: s.now(),
	})
	if err != nil {
		return nil, "", s.approvalError(err)
	}
	// Surface a request no gate will ever consume, instead of letting it
	// linger silently.
	warning := ""
	if scope == approval.ScopeStageGate {
		if res, perr := s.policyForWorkItem(ctx, wi.Item); perr == nil && res.Policy != nil {
			gate, declared := res.Policy.Gates.Stages[req.Stage]
			if !declared || !gate.RequireApproval {
				warning = fmt.Sprintf("policy %q declares no require_approval gate for stage %q; nothing will consume this approval", res.Policy.ID, req.Stage)
			}
		}
	}
	return apr, warning, nil
}

// DecideApprovalRequest is the input to ApprovalApprove / ApprovalReject.
type DecideApprovalRequest struct {
	ID      string
	By      string
	Comment string
	Reason  string
}

// ApprovalApprove records an approval decision.
func (s *Service) ApprovalApprove(ctx context.Context, req DecideApprovalRequest) (*domain.Approval, error) {
	if req.ID == "" || req.By == "" {
		return nil, Usagef("approve requires id and by")
	}
	apr, err := approval.New(s.Root).Decide(ctx, req.ID, approval.DecideOptions{
		Approve: true, DecidedBy: req.By, Comment: req.Comment, Now: s.now(),
	})
	if err != nil {
		return nil, s.approvalError(err)
	}
	return apr, nil
}

// ApprovalReject rejects an approval. A stage-gate rejection commits the
// decision and the work item disposition in ONE transaction (the decision
// rides the transition's Guard), so a failed disposition leaves the approval
// pending instead of half-applying (方案 §4.9). The returned disposition is
// the status the work item moved to ("" for action-scope approvals).
func (s *Service) ApprovalReject(ctx context.Context, req DecideApprovalRequest) (*domain.Approval, string, error) {
	if req.ID == "" || req.By == "" || req.Reason == "" {
		return nil, "", Usagef("reject requires id, by and reason")
	}
	store := approval.New(s.Root)
	apr, err := store.Get(ctx, req.ID)
	if err != nil {
		return nil, "", s.approvalError(err)
	}
	disposition := ""
	if apr.Scope == approval.ScopeStageGate {
		disposition, err = s.rejectStageGate(ctx, apr, req.By, req.Reason)
		if err != nil {
			return nil, "", err
		}
	} else if _, err := store.Decide(ctx, req.ID, approval.DecideOptions{
		Approve: false, DecidedBy: req.By, Comment: req.Reason, Now: s.now(),
	}); err != nil {
		return nil, "", s.approvalError(err)
	}
	apr, err = store.Get(ctx, req.ID)
	if err != nil {
		return nil, "", s.approvalError(err)
	}
	return apr, disposition, nil
}

// rejectStageGate records the rejection and moves the work item (default
// blocked, or the policy's on_reject regress target) in a single transition
// transaction. On any refusal nothing is committed — the approval stays
// pending and the error explains what blocked the disposition.
func (s *Service) rejectStageGate(ctx context.Context, apr *domain.Approval, decider, reason string) (string, error) {
	items := s.items()
	wi, raw, err := items.ReadSnapshot(ctx, apr.WorkItemID)
	if err != nil {
		return "", s.storeError(err)
	}
	target := domain.StatusBlocked
	if pol, perr := s.policyForWorkItem(ctx, wi); perr == nil && pol.Policy != nil && strings.HasPrefix(pol.Policy.OnReject, "regress:") {
		target = strings.TrimPrefix(pol.Policy.OnReject, "regress:")
	}
	now := s.now()
	guard := func(tx *storage.Tx) ([]*domain.Event, error) {
		_, ev, derr := approval.DecideTx(tx, apr.ID, approval.DecideOptions{
			Approve: false, DecidedBy: decider, Comment: reason, Now: now,
		})
		if derr != nil {
			return nil, derr
		}
		return []*domain.Event{ev}, nil
	}
	if _, err := items.Transition(ctx, wi.ID, workitem.TransitionRequest{
		TargetStatus: target,
		Actor:        decider,
		Reason:       fmt.Sprintf("approval %s rejected: %s", apr.ID, reason),
		Now:          now,
		Guard:        guard,
	}, raw); err != nil {
		return "", Invalidf(KindApproval, nil,
			"rejection not recorded: work item %s could not move to %s (%v); nothing was changed", apr.WorkItemID, target, err)
	}
	return target, nil
}

func (s *Service) approvalError(err error) error {
	if errors.Is(err, storage.ErrNotInitialized) || errors.Is(err, approval.ErrNotFound) {
		return Preconditionf("%v", err)
	}
	return Invalidf(KindApproval, nil, "%v", err)
}
