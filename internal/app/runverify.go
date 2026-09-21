package app

import (
	"context"
	"errors"
	"fmt"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/run"
	"workloom/internal/workitem"
	"workloom/internal/workspace"
)

// CompletionCheck is the evidence behind a completion: the head the attempt
// started from and the head its branch points at now (方案 §4.8).
type CompletionCheck struct {
	RunID       string `json:"run_id"`
	Workspace   string `json:"workspace,omitempty"`
	Branch      string `json:"branch,omitempty"`
	ClaimHead   string `json:"claim_head,omitempty"`
	CurrentHead string `json:"current_head,omitempty"`
	Advanced    bool   `json:"advanced"`
	Reason      string `json:"reason,omitempty"`
}

// RunVerify reports whether an attempt's work advanced its branch, without
// changing anything: the read-only half of the completion check.
func (s *Service) RunVerify(ctx context.Context, runID string) (CompletionCheck, error) {
	r, _, err := readRun(ctx, s, runID)
	if err != nil {
		return CompletionCheck{}, err
	}
	return s.verifyCompletion(ctx, r), nil
}

// verifyCompletion compares the claim head with the branch the attempt worked
// on. "No evidence" is never "verified": a missing claim head, a missing
// workspace or a git failure all report Advanced=false with the reason.
func (s *Service) verifyCompletion(ctx context.Context, r *domain.Run) CompletionCheck {
	check := CompletionCheck{
		RunID: r.ID, Workspace: r.Workspace.Path, Branch: r.Workspace.Branch,
		ClaimHead: r.Claim.HeadSHA,
	}
	// Without a workspace there is nothing to compare against: falling back to
	// the project root would "verify" an attempt against a branch it never
	// worked on.
	if r.Workspace.Path == "" {
		check.Reason = "the attempt has no workspace, so there is no evidence it advanced anything"
		return check
	}
	if r.Workspace.Branch == "" {
		check.Reason = "the attempt has no workspace branch"
		return check
	}
	current, err := workspace.BranchSHA(r.Workspace.Path, r.Workspace.Branch)
	if err != nil {
		check.Reason = fmt.Sprintf("cannot read the branch head of %s: %v", r.Workspace.Branch, err)
		return check
	}
	check.CurrentHead = current
	switch {
	case check.ClaimHead == "":
		check.Reason = "the attempt has no claim head, so there is no evidence it advanced anything"
	case current == "":
		check.Reason = "the current head is empty"
	case current == check.ClaimHead:
		check.Reason = fmt.Sprintf("the branch still points at %s: nothing was committed", current)
	default:
		check.Advanced = true
	}
	return check
}

// refuseCompletion records a completion the check refused: the run keeps its
// evidence (advanced=false and the head it stopped at), the work item goes to
// human review, and the event says why. The run stays non-terminal so the
// attempt can still be finished by whoever reviews it.
func (s *Service) refuseCompletion(ctx context.Context, r *domain.Run, actor, reason string, check CompletionCheck) error {
	fresh, raw, err := readRun(ctx, s, r.ID)
	if err != nil {
		return err
	}
	advanced := false
	fresh.Verification.Advanced = &advanced
	fresh.Verification.HeadSHAAtComplete = check.CurrentHead
	if err := run.New(s.Root).Update(ctx, fresh, raw); err != nil {
		return s.storeError(err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "completion_refused",
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   fmt.Sprintf("claim head %s, current head %s: %s", orNone(check.ClaimHead), orNone(check.CurrentHead), reason),
		Time:      s.now(),
	}); err != nil {
		return s.storeError(err)
	}
	return s.routeToReview(ctx, r, actor, reason)
}

// routeToReview moves the work item into human review: the queue is the review
// status itself, which next and workitem list already surface.
func (s *Service) routeToReview(ctx context.Context, r *domain.Run, actor, reason string) error {
	wi, raw, err := s.readSnapshot(ctx, r.WorkItemID, "")
	if err != nil {
		return err
	}
	// The attempt asked to complete, so it is over: give the claim back
	// first, whatever the item's status is — a claimed item refuses
	// transitions from anyone but its lease holder, and the reviewer must
	// not need the agent's token. The release is bound to this run's
	// recorded refusal, so it cannot touch an unrelated active claim.
	lease, err := s.items().LeaseInspection(ctx, wi.ID)
	switch {
	case err == nil && lease.Owner != "":
		if err := s.items().Release(ctx, wi.ID, workitem.ReleaseOptions{
			ForRefused: true,
			BoundRunID: r.ID,
			Actor:      actor,
			Reason:     "completion refused; claim released for review",
			Now:        s.now(),
		}); err != nil {
			return err
		}
		if wi, raw, err = s.readSnapshot(ctx, r.WorkItemID, ""); err != nil {
			return err
		}
	case err != nil && !errors.Is(err, workitem.ErrNotClaimed):
		return s.storeError(err)
	}
	if wi.Status == domain.StatusReview {
		return nil
	}
	if !domain.IsTransitionLegal(wi.Status, domain.StatusReview) {
		// A work item that cannot enter review (already closed, say) keeps its
		// state: the refusal is still recorded on the run and in the event log.
		return nil
	}
	// The automatic route goes through the same stage gate as a manual
	// transition: a refuse-to-review must not become a way around
	// require_comment / require_artifacts (#342). When the gate blocks, the
	// claim stays released (the reviewer needs no token) and the item keeps
	// its status; the event names the gate so the trail explains why.
	if _, _, gerr := s.checkTransitionGate(ctx, wi, domain.StatusReview); gerr != nil {
		var ae *Error
		if errors.As(gerr, &ae) && ae.Kind == KindGate {
			return events.New(s.Root).Append(ctx, &domain.Event{
				Type:      "review_gate_blocked",
				Subject:   domain.Reference{Type: "workitem", ID: wi.ID},
				ProjectID: wi.ProjectID,
				Actor:     actor,
				Related:   []domain.Reference{{Type: "run", ID: r.ID}},
				Content:   gerr.Error(),
				Time:      s.now(),
			})
		}
		return gerr
	}
	if _, err := s.items().Transition(ctx, wi.ID, workitem.TransitionRequest{
		TargetStatus: domain.StatusReview,
		Actor:        actor,
		Reason:       "completion refused: " + reason,
	}, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}

func orNone(sha string) string {
	if sha == "" {
		return "(none)"
	}
	return sha
}

// runUpdateRaw writes a run as the fixtures need it: the completion check must
// cope with state that was edited by hand or left half-bound.
func (s *Service) runUpdateRaw(r *domain.Run, expect string) error {
	_, raw, err := readRun(context.Background(), s, r.ID)
	if err != nil {
		return err
	}
	if expect != "" {
		if err := checkExpect(expect, raw, "run"); err != nil {
			return err
		}
	}
	if err := run.New(s.Root).Update(context.Background(), r, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}
