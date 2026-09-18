package app

import (
	"context"
	"fmt"

	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/workflow"
	"workloom/internal/workitem"
)

// WorkflowView is one work item's workflow instance plus, for read
// operations, the evaluated step candidates.
type WorkflowView struct {
	WorkitemID string                   `json:"workitem_id"`
	Workflow   *domain.WorkflowInstance `json:"workflow"`
	Candidates []workflow.StepCandidate `json:"candidates,omitempty"`
	Notice     string                   `json:"notice,omitempty"`
}

// PolicySummary is the JSON view of one valid policy file.
type PolicySummary struct {
	File    string `json:"file"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// WorkflowList scans the policy files (the data behind `workflow check`):
// valid policies plus every located issue, errors and warnings apart.
func (s *Service) WorkflowList(ctx context.Context) ([]PolicySummary, []workflow.Issue, error) {
	if !dirExists(s.Root + "/.devsys") {
		return nil, nil, Preconditionf("no .devsys/ in %s: run `devsys init` first", s.Root)
	}
	results := workflow.Load(s.Root)
	var summaries []PolicySummary
	var warnings []workflow.Issue
	var problems []config.Problem
	for _, res := range results {
		if res.Policy != nil {
			summaries = append(summaries, PolicySummary{
				File: res.File, ID: res.Policy.ID, Name: res.Policy.Name, Version: res.Policy.Version,
			})
		}
		for _, is := range res.Issues {
			switch is.Severity {
			case workflow.SeverityError:
				problems = append(problems, config.Problem{File: is.File, Line: is.Line, Field: is.Field, Reason: is.Reason})
			case workflow.SeverityWarning:
				warnings = append(warnings, is)
			}
		}
	}
	if len(problems) > 0 {
		return nil, warnings, Invalidf(KindInvalid, problems, "invalid workflow policies (%d problems)", len(problems))
	}
	return summaries, warnings, nil
}

// WorkflowGet reads one work item's instance and, when the policy resolves,
// its step candidates.
func (s *Service) WorkflowGet(ctx context.Context, id string) (WorkflowView, error) {
	wi, err := s.WorkitemGet(ctx, id)
	if err != nil {
		return WorkflowView{}, err
	}
	view := WorkflowView{WorkitemID: wi.Item.ID, Workflow: wi.Item.Workflow}
	if wi.Item.Workflow == nil {
		return view, nil
	}
	pol, notice, err := s.resolvePolicy(ctx, wi.Item, "", false)
	if err != nil {
		return view, err
	}
	view.Notice = notice
	if pol == nil {
		return view, nil
	}
	_, candidates, err := s.items().WorkflowNext(ctx, id, pol)
	if err != nil {
		return view, s.mapWorkflowError(err, pol.File)
	}
	view.Candidates = candidates
	return view, nil
}

// WorkflowStart attaches and starts a workflow instance (dispatch-like: the
// policy must be currently valid).
func (s *Service) WorkflowStart(ctx context.Context, id, policyID, actor, reason, owner, token, expect string) (WorkflowView, error) {
	if policyID == "" || actor == "" || reason == "" {
		return WorkflowView{}, Usagef("workflow start requires policy, actor and reason")
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkflowView{}, err
	}
	pol, _, err := s.resolvePolicy(ctx, wi, policyID, true)
	if err != nil {
		return WorkflowView{}, err
	}
	updated, err := s.items().WorkflowStart(ctx, id, workitem.WorkflowStartOptions{
		Policy: pol, Actor: actor, Reason: reason, Owner: owner, Token: token, Expected: expected,
	})
	if err != nil {
		return WorkflowView{}, s.mapWorkflowError(err, pol.File)
	}
	return WorkflowView{WorkitemID: updated.ID, Workflow: updated.Workflow}, nil
}

// WorkflowStepNext evaluates the step candidates without writing anything.
func (s *Service) WorkflowStepNext(ctx context.Context, id string) (WorkflowView, error) {
	wi, _, err := s.readSnapshot(ctx, id, "")
	if err != nil {
		return WorkflowView{}, err
	}
	pol, notice, err := s.resolvePolicy(ctx, wi, "", false)
	if err != nil {
		return WorkflowView{}, err
	}
	updated, candidates, err := s.items().WorkflowNext(ctx, id, pol)
	if err != nil {
		return WorkflowView{}, s.mapWorkflowError(err, pol.File)
	}
	return WorkflowView{WorkitemID: updated.ID, Workflow: updated.Workflow, Candidates: candidates, Notice: notice}, nil
}

// WorkflowStepComplete advances the instance (declaration order unless a
// target step is named); refusals carry the allowed candidates.
func (s *Service) WorkflowStepComplete(ctx context.Context, id, to, actor, reason, owner, token, expect string) (WorkflowView, error) {
	if actor == "" || reason == "" {
		return WorkflowView{}, Usagef("workflow step-complete requires actor and reason")
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkflowView{}, err
	}
	pol, notice, err := s.resolvePolicy(ctx, wi, "", false)
	if err != nil {
		return WorkflowView{}, err
	}
	updated, err := s.items().WorkflowStepComplete(ctx, id, workitem.WorkflowStepOptions{
		Policy: pol, To: to, Actor: actor, Reason: reason, Owner: owner, Token: token, Expected: expected,
	})
	if err != nil {
		// The caller still sees the last-known-good fallback: a refusal on a
		// policy that is not currently valid must say so (M3.5).
		return WorkflowView{WorkitemID: id, Notice: notice}, s.mapWorkflowError(err, pol.File)
	}
	return WorkflowView{WorkitemID: updated.ID, Workflow: updated.Workflow, Notice: notice}, nil
}

// WorkflowSignal pauses, resumes or cancels an instance.
func (s *Service) WorkflowSignal(ctx context.Context, action, id, actor, reason, owner, token, expect string) (WorkflowView, error) {
	if actor == "" || reason == "" {
		return WorkflowView{}, Usagef("workflow %s requires actor and reason", action)
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkflowView{}, err
	}
	// Signals need no policy semantics, but the policy file locates refusals
	// when a candidate list is rendered, and an invalid file still surfaces
	// its last-known-good fallback.
	policyFile := ""
	notice := ""
	if pol, n, perr := s.resolvePolicy(ctx, wi, "", false); perr == nil && pol != nil {
		policyFile = pol.File
		notice = n
	}
	signal := workitem.WorkflowSignalOptions{
		Actor: actor, Reason: reason, Owner: owner, Token: token, Expected: expected,
	}
	var updated *domain.WorkItem
	switch action {
	case "pause":
		updated, err = s.items().WorkflowPause(ctx, id, signal)
	case "resume":
		updated, err = s.items().WorkflowResume(ctx, id, signal)
	case "cancel":
		updated, err = s.items().WorkflowCancel(ctx, id, signal)
	default:
		return WorkflowView{}, Usagef("unknown workflow action %q", action)
	}
	if err != nil {
		return WorkflowView{}, s.mapWorkflowError(err, policyFile)
	}
	return WorkflowView{WorkitemID: updated.ID, Workflow: updated.Workflow, Notice: notice}, nil
}

// resolvePolicy resolves the policy an instance operation consumes. start is
// dispatch-like and requires a currently valid file; advancing and read
// operations accept the last-known-good snapshot and report the fallback in
// the returned notice (实施计划 M3.5).
func (s *Service) resolvePolicy(ctx context.Context, wi *domain.WorkItem, policyID string, requireCurrent bool) (*workflow.Policy, string, error) {
	id := policyID
	if id == "" && wi.Workflow != nil {
		id = wi.Workflow.ID
	}
	if id == "" {
		return nil, "", Usagef("--policy <id> is required when the work item has no workflow instance")
	}
	res, err := workflow.Resolve(ctx, s.Root, id)
	if err != nil {
		return nil, "", s.errWorkflowPolicy(err)
	}
	notice := ""
	if res.Issue != nil {
		notice = fmt.Sprintf("workflow policy %q is invalid: %s; using %s", res.ID, res.Issue.String(), res.Source)
		if requireCurrent {
			return nil, "", s.errWorkflowPolicy(fmt.Errorf(
				"workflow policy %q is invalid: %s; the operation is blocked until the file is fixed (%s is retained for read paths)",
				res.ID, res.Issue.String(), res.Source))
		}
	}
	return res.Policy, notice, nil
}
