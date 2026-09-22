package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/approval"
	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/harness"
	"github.com/JAYY513/Workloom/internal/record"
	"github.com/JAYY513/Workloom/internal/run"
	"github.com/JAYY513/Workloom/internal/storage"
	"github.com/JAYY513/Workloom/internal/workflow"
	"github.com/JAYY513/Workloom/internal/workitem"
)

// WorkItemView is one work item plus the version hash every mutation
// consumes (方案 §15.2 乐观并发).
type WorkItemView struct {
	Item    *domain.WorkItem `json:"item"`
	Version string           `json:"version"`
}

// ClaimView reports the claim's fresh run and lease credentials.
type ClaimView struct {
	WorkitemID string `json:"workitem_id"`
	RunID      string `json:"run_id"`
	Token      string `json:"token"`
	Status     string `json:"status"`
	// Notice carries a non-fatal warning for the caller to surface: today the
	// "no workflow policy governs this work item" gap (gates not enforced).
	Notice string `json:"notice,omitempty"`
}

// claimNotice explains that a claimed work item is governed by no policy at
// all while the project declares policies: the quality gate and the stage
// gates then never ran, which is exactly how a claim could bypass them.
func (s *Service) claimNotice(ctx context.Context, wi *domain.WorkItem) string {
	res, err := s.policyForWorkItem(ctx, wi)
	if err == nil && res.Policy != nil {
		return ""
	}
	id := ""
	if wi.Workflow != nil {
		id = wi.Workflow.ID
	}
	if id != "" {
		// A bound policy that cannot be resolved already blocks the claim
		// (checkClaimQuality runs first); nothing to add here.
		return ""
	}
	if len(policyIDs(workflow.Load(s.Root))) == 0 {
		// No policy exists in the project: nothing is being bypassed.
		return ""
	}
	return fmt.Sprintf(
		"%s has no workflow instance and .devsys/config.yaml declares no default_policy: quality and stage gates are not enforced for it; bind one with `workloom workflow start --id %s --policy <id>` or set `default_policy` in .devsys/config.yaml",
		wi.ID, wi.ID)
}

// WorkitemFilter narrows a work item listing. Zero values mean no filter.
type WorkitemFilter struct {
	Status string
	Type   string
	Parent string
}

// WorkitemList returns the work items matching the filter, in store order.
func (s *Service) WorkitemList(ctx context.Context, filter WorkitemFilter) ([]*domain.WorkItem, error) {
	items, err := s.items().List(ctx)
	if err != nil {
		return nil, s.storeError(err)
	}
	if items == nil {
		items = []*domain.WorkItem{}
	}
	if filter.Status == "" && filter.Type == "" && filter.Parent == "" {
		return items, nil
	}
	out := make([]*domain.WorkItem, 0, len(items))
	for _, wi := range items {
		if filter.Status != "" && wi.Status != filter.Status {
			continue
		}
		if filter.Type != "" && wi.Type != filter.Type {
			continue
		}
		if filter.Parent != "" && (wi.ParentID == nil || *wi.ParentID != filter.Parent) {
			continue
		}
		out = append(out, wi)
	}
	return out, nil
}

// WorkitemGet reads one work item with its version hash.
func (s *Service) WorkitemGet(ctx context.Context, id string) (WorkItemView, error) {
	wi, raw, err := s.items().ReadSnapshot(ctx, id)
	if err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return WorkItemView{Item: wi, Version: versionHash(raw)}, nil
}

// CreateWorkitemRequest is the input to WorkitemCreate.
type CreateWorkitemRequest struct {
	Title              string
	Description        string
	Type               string
	Prefix             string
	ParentID           string
	Priority           int
	Actor              string
	Reason             string
	AcceptanceCriteria []string
}

// WorkitemCreate creates a draft work item (ID assignment is CAS-guarded).
func (s *Service) WorkitemCreate(ctx context.Context, req CreateWorkitemRequest) (WorkItemView, error) {
	md, err := s.project()
	if err != nil {
		return WorkItemView{}, err
	}
	if strings.TrimSpace(req.Title) == "" || req.Actor == "" || req.Reason == "" {
		return WorkItemView{}, Usagef("workitem create requires title, actor and reason")
	}
	kind := req.Type
	if kind == "" {
		kind = "task"
	}
	prefix := req.Prefix
	if prefix == "" {
		prefix = "WLM"
	}
	now := s.now()
	wi := &domain.WorkItem{
		ProjectID:  md.Project.ID,
		Type:       kind,
		Title:      req.Title,
		Status:     domain.StatusDraft,
		Priority:   req.Priority,
		ProposedBy: req.Actor,
		Reason:     req.Reason,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if req.Description != "" {
		wi.Description = req.Description
	}
	// Acceptance criteria are set at create so a work item can satisfy a
	// policy's quality gate without a second `workitem update` call.
	if len(req.AcceptanceCriteria) > 0 {
		wi.AcceptanceCriteria = req.AcceptanceCriteria
	}
	if req.ParentID != "" {
		parent := req.ParentID
		wi.ParentID = &parent
	}
	id, err := s.items().Create(ctx, wi, prefix)
	if err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return s.WorkitemGet(ctx, id)
}

// UpdateWorkitemRequest patches mutable work item fields. Nil fields are
// left untouched; Expect is the version hash from WorkitemGet.
type UpdateWorkitemRequest struct {
	Title              *string
	Description        *string
	Priority           *int
	AcceptanceCriteria []string
	Dependencies       []string
	Constraints        []string
	// AssignedAgent and AssignedHarness say who should work the item; the
	// harness is what dispatch resolves an adapter from (方案 §9.3).
	AssignedAgent   *string
	AssignedHarness *string
	Expect          string
	// Actor and Reason are the mandatory audit trail for the write.
	Actor  string
	Reason string
}

// WorkitemUpdate applies a patch under the version guard. Actor and reason
// are mandatory: every write carries an audit trail (手册「凡写命令都要
// actor+reason」), recorded as a workitem_updated event.
func (s *Service) WorkitemUpdate(ctx context.Context, id string, req UpdateWorkitemRequest) (WorkItemView, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.Reason) == "" {
		return WorkItemView{}, Usagef("workitem update requires --actor and --reason (audit trail)")
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		return WorkItemView{}, Usagef("title must not be empty")
	}
	if req.Title == nil && req.Description == nil && req.Priority == nil &&
		req.AcceptanceCriteria == nil && req.Dependencies == nil && req.Constraints == nil &&
		req.AssignedAgent == nil && req.AssignedHarness == nil {
		return WorkItemView{}, Usagef("workitem update requires at least one field to change")
	}
	wi, expected, err := s.readSnapshot(ctx, id, req.Expect)
	if err != nil {
		return WorkItemView{}, err
	}
	var fields []string
	if req.Title != nil {
		wi.Title = *req.Title
		fields = append(fields, "title")
	}
	if req.Description != nil {
		wi.Description = *req.Description
		fields = append(fields, "description")
	}
	if req.Priority != nil {
		wi.Priority = *req.Priority
		fields = append(fields, "priority")
	}
	if req.AcceptanceCriteria != nil {
		wi.AcceptanceCriteria = req.AcceptanceCriteria
		fields = append(fields, "acceptance")
	}
	if req.Dependencies != nil {
		wi.Dependencies = req.Dependencies
		fields = append(fields, "dependencies")
	}
	if req.AssignedAgent != nil {
		wi.AssignedAgent = trimmedOrNil(req.AssignedAgent)
		fields = append(fields, "assigned-agent")
	}
	if req.AssignedHarness != nil {
		name := strings.TrimSpace(*req.AssignedHarness)
		if name != "" {
			if _, ok := harness.ByName(name); !ok {
				return WorkItemView{}, Usagef("unknown harness %q (known: %s)", name, strings.Join(harness.Names(), ", "))
			}
		}
		wi.AssignedHarness = trimmedOrNil(req.AssignedHarness)
		fields = append(fields, "assigned-harness")
	}
	if req.Constraints != nil {
		wi.Constraints = req.Constraints
		fields = append(fields, "constraints")
	}
	wi.UpdatedAt = s.now()
	if err := s.items().Update(ctx, wi, expected); err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	ev := &domain.Event{
		Type:      "workitem_updated",
		Subject:   domain.Reference{Type: "workitem", ID: wi.ID},
		ProjectID: wi.ProjectID,
		Actor:     req.Actor,
		Content:   fmt.Sprintf("fields: %s — %s", strings.Join(fields, ", "), req.Reason),
		Time:      s.now(),
	}
	if err := events.New(s.Root).Append(ctx, ev); err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return s.WorkitemGet(ctx, id)
}

// WorkitemTransition advances the state machine through the stage gate (and
// consumes the matching approval in the same transaction). The returned
// notice carries a last-known-good fallback note for the caller to surface.
func (s *Service) WorkitemTransition(ctx context.Context, id, to, actor, reason, expect string) (WorkItemView, string, error) {
	if to == "" || actor == "" || reason == "" {
		return WorkItemView{}, "", Usagef("transition requires to, actor and reason")
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkItemView{}, "", err
	}
	notice, approvalID, err := s.checkTransitionGate(ctx, wi, to)
	if err != nil {
		return WorkItemView{}, notice, err
	}
	req := workitem.TransitionRequest{
		TargetStatus: to, Actor: actor, Reason: reason, Now: s.now(),
	}
	if approvalID != "" {
		// The approval is consumed inside the transition transaction: it only
		// becomes consumed when the advance it authorized commits (M3.7), and
		// its event joins the same batch with the same timestamp.
		consumeID, stage, fromStatus, at := approvalID, to, wi.Status, req.Now
		req.Guard = func(tx *storage.Tx) ([]*domain.Event, error) {
			ev, err := approval.ConsumeTx(tx, consumeID, wi.ID, stage, fromStatus, at, actor, reason)
			if err != nil {
				return nil, err
			}
			return []*domain.Event{ev}, nil
		}
	}
	if _, err := s.items().Transition(ctx, id, req, expected); err != nil {
		return WorkItemView{}, notice, s.storeError(err)
	}
	view, err := s.WorkitemGet(ctx, id)
	return view, notice, err
}

// WorkitemClaim claims a ready work item after the claim quality gate, and
// returns the fresh run credentials.
func (s *Service) WorkitemClaim(ctx context.Context, id, owner, reason, expect string) (ClaimView, error) {
	if owner == "" || reason == "" {
		return ClaimView{}, Usagef("claim requires owner and reason")
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return ClaimView{}, err
	}
	if err := s.checkClaimQuality(ctx, wi); err != nil {
		return ClaimView{}, err
	}
	res, err := s.items().Claim(ctx, id, workitem.ClaimOptions{
		Owner: owner, Actor: owner, Reason: reason, Expected: expected, Now: s.now(),
	})
	if err != nil {
		return ClaimView{}, s.storeError(err)
	}
	return ClaimView{
		WorkitemID: id, RunID: res.RunID, Token: res.Token, Status: res.Status,
		Notice: s.claimNotice(ctx, wi),
	}, nil
}

// WorkitemRelease releases an active lease (owner and token must match).
func (s *Service) WorkitemRelease(ctx context.Context, id, owner, token, actor, reason, expect string) (WorkItemView, error) {
	if owner == "" || token == "" || actor == "" || reason == "" {
		return WorkItemView{}, Usagef("release requires owner, token, actor and reason")
	}
	_, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkItemView{}, err
	}
	if err := s.items().Release(ctx, id, workitem.ReleaseOptions{
		Owner: owner, Token: token, Expected: expected, Actor: actor, Reason: reason, Now: s.now(),
	}); err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return s.WorkitemGet(ctx, id)
}

// WorkitemStart moves a claimed work item into its run (owner and token must
// match the active lease).
func (s *Service) WorkitemStart(ctx context.Context, id, owner, token, actor, reason, expect string) (ClaimView, error) {
	if owner == "" || token == "" || actor == "" || reason == "" {
		return ClaimView{}, Usagef("start requires owner, token, actor and reason")
	}
	_, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return ClaimView{}, err
	}
	res, err := s.items().Start(ctx, id, workitem.StartOptions{
		Owner: owner, Token: token, Expected: expected, Actor: actor, Reason: reason, Now: s.now(),
	})
	if err != nil {
		return ClaimView{}, s.storeError(err)
	}
	view, err := s.WorkitemGet(ctx, id)
	if err != nil {
		return ClaimView{}, err
	}
	return ClaimView{WorkitemID: id, RunID: res.RunID, Token: res.Token, Status: view.Item.Status}, nil
}

// CommentView reports one recorded comment event.
type CommentView struct {
	Event *domain.Event `json:"event"`
}

// WorkitemComment appends a comment event on a work item (方案 §4.6).
func (s *Service) WorkitemComment(ctx context.Context, id, text, actor, replyTo string) (CommentView, error) {
	if strings.TrimSpace(text) == "" || actor == "" {
		return CommentView{}, Usagef("comment requires text and actor")
	}
	wi, err := s.WorkitemGet(ctx, id)
	if err != nil {
		return CommentView{}, err
	}
	ev := &domain.Event{
		Type:      "comment",
		Subject:   domain.Reference{Type: "workitem", ID: wi.Item.ID},
		ProjectID: wi.Item.ProjectID,
		Actor:     actor,
		Content:   text,
		Time:      s.now(),
	}
	if replyTo != "" {
		reply := replyTo
		ev.ReplyTo = &reply
	}
	if err := events.New(s.Root).Append(ctx, ev); err != nil {
		return CommentView{}, s.storeError(err)
	}
	return CommentView{Event: ev}, nil
}

// WorkitemAddDependency records that id depends on dependsOn.
func (s *Service) WorkitemAddDependency(ctx context.Context, id, dependsOn, expect string) (WorkItemView, error) {
	return s.mutateDependencies(ctx, id, expect, func(wi *domain.WorkItem, deps []string) ([]string, error) {
		for _, d := range deps {
			if d == dependsOn {
				return nil, Usagef("work item %s already depends on %s", wi.ID, dependsOn)
			}
		}
		return append(deps, dependsOn), nil
	}, dependsOn)
}

// WorkitemRemoveDependency drops a recorded dependency.
func (s *Service) WorkitemRemoveDependency(ctx context.Context, id, dependsOn, expect string) (WorkItemView, error) {
	return s.mutateDependencies(ctx, id, expect, func(wi *domain.WorkItem, deps []string) ([]string, error) {
		out := make([]string, 0, len(deps))
		found := false
		for _, d := range deps {
			if d == dependsOn {
				found = true
				continue
			}
			out = append(out, d)
		}
		if !found {
			return nil, Usagef("work item %s does not depend on %s", wi.ID, dependsOn)
		}
		return out, nil
	}, dependsOn)
}

// mutateDependencies validates the target once and applies the mutation
// under the version guard.
func (s *Service) mutateDependencies(ctx context.Context, id, expect string, mutate func(*domain.WorkItem, []string) ([]string, error), dependsOn string) (WorkItemView, error) {
	if dependsOn == "" {
		return WorkItemView{}, Usagef("dependency target is required")
	}
	if dependsOn == id {
		return WorkItemView{}, Usagef("a work item cannot depend on itself")
	}
	wi, expected, err := s.readSnapshot(ctx, id, expect)
	if err != nil {
		return WorkItemView{}, err
	}
	if _, err := s.WorkitemGet(ctx, dependsOn); err != nil {
		return WorkItemView{}, err
	}
	deps := append([]string(nil), wi.Dependencies...)
	deps, err = mutate(wi, deps)
	if err != nil {
		return WorkItemView{}, err
	}
	wi.Dependencies = deps
	wi.UpdatedAt = s.now()
	if err := s.items().Update(ctx, wi, expected); err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return s.WorkitemGet(ctx, id)
}

// readSnapshot reads a work item and turns the expect hash into the guard the
// mutations require. An empty expect keeps the caller's existing CLI
// affordance (read-then-write discipline is enforced whenever a hash is
// supplied); a stale or malformed hash never grants authority (fail closed).
func (s *Service) readSnapshot(ctx context.Context, id, expect string) (*domain.WorkItem, []byte, error) {
	wi, raw, err := s.items().ReadSnapshot(ctx, id)
	if err != nil {
		return nil, nil, s.storeError(err)
	}
	if expect = strings.TrimSpace(expect); expect != "" {
		want, derr := hex.DecodeString(expect)
		sum := storage.HashBytes(raw)
		if derr != nil || len(want) != sha256.Size || !bytes.Equal(want, sum[:]) {
			return nil, nil, Invalidf(KindWorkitem, nil,
				"version mismatch: work item changed since your read; rerun workitem get")
		}
	}
	return wi, raw, nil
}

// storeError classifies domain/storage failures the way the CLI always did:
// uninitialized projects and missing records are preconditions, everything
// else is untrusted state.
// trimmedOrNil keeps an empty assignment empty (nil) instead of a pointer to
// an empty string, so "no harness" has one representation.
func trimmedOrNil(value *string) *string {
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *Service) storeError(err error) error {
	if err == nil {
		return nil
	}
	// A malformed work item id is a caller error, not untrusted state: report
	// it as a usage error (exit 2) instead of "invalid managed state".
	if errors.Is(err, workitem.ErrBadID) {
		return Usagef("%v", err)
	}
	if errors.Is(err, storage.ErrNotInitialized) || errors.Is(err, workitem.ErrNotFound) ||
		errors.Is(err, approval.ErrNotFound) || errors.Is(err, record.ErrNotFound) ||
		errors.Is(err, run.ErrNotFound) {
		return Preconditionf("%v", err)
	}
	if errors.Is(err, storage.ErrConflict) {
		// 方案 §15.2 乐观并发: the record moved after the caller's snapshot.
		// Name the way out, because "version conflict" alone leaves the
		// operator guessing (a dispatch child writing the run is the usual
		// cause of a failure reported by `run fail`).
		return Invalidf(KindWorkitem, nil,
			"%v (the file changed after your read: re-read it and retry, or repeat with --latest where the command offers it)", err)
	}
	return Invalidf(KindWorkitem, nil, "%v", err)
}

func versionHash(raw []byte) string {
	sum := storage.HashBytes(raw)
	return hex.EncodeToString(sum[:])
}

// policyForWorkItem resolves the workflow policy governing a work item: the
// policy its instance declares, else the project-level default_policy from
// .devsys/config.yaml (方案 §4.7/§5.3 — a project-wide fallback so claiming
// first and binding a policy later is not a way around the gates). A work item
// under neither resolves to an empty Resolution. The current file is
// re-validated on every call; a failing file falls back to the last-known-good
// snapshot with the current failure reported in Resolution.Issue (实施计划 M3.5).
func (s *Service) policyForWorkItem(ctx context.Context, wi *domain.WorkItem) (workflow.Resolution, error) {
	id := ""
	if wi.Workflow != nil {
		id = wi.Workflow.ID
		if id == "" {
			return workflow.Resolution{}, Internalf("work item %s has a workflow instance without an id", wi.ID)
		}
	}
	if id == "" {
		md, problems := config.Load(s.Root)
		for _, p := range problems {
			if p.File != config.ConfigFile {
				continue
			}
			// config.yaml carries default_policy: an unreadable file would
			// silently drop the project's gates, so refuse instead of
			// claiming under no policy (方案 §5.3: 配置错误阻塞新任务派发).
			return workflow.Resolution{}, fmt.Errorf(
				"%s is invalid: %s; claims stay blocked until the file is fixed (`workloom config check`)", config.ConfigFile, p.String())
		}
		if md == nil || md.Config == nil {
			return workflow.Resolution{}, nil
		}
		id = strings.TrimSpace(md.Config.DefaultPolicy)
		if id == "" {
			return workflow.Resolution{}, nil
		}
	}
	res, err := workflow.Resolve(ctx, s.Root, id)
	if err != nil {
		return workflow.Resolution{}, err
	}
	return res, nil
}

// policyIDs names the workflow policies a workflow.Load result carries, by
// file name. A file that fails to parse still counts: the project intends that
// workflow, and Load reports the parse error separately.
func policyIDs(policies []workflow.FileResult) []string {
	var out []string
	for _, res := range policies {
		if !strings.HasSuffix(res.File, ".md") {
			continue // the workflows/ directory itself, or a non-policy entry
		}
		name := res.File
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		out = append(out, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(out)
	return out
}

// gateEvidence collects the evidence a stage gate consumes: artifact names,
// comment events and, for require_approval gates, the approved unconsumed
// approval matching the stage and the work item's current status (方案 §4.9).
// The matching approval id is returned so the advancing transaction can
// consume it atomically.
func (s *Service) gateEvidence(ctx context.Context, wi *domain.WorkItem, stage string) (workflow.GateEvidence, string, error) {
	artifacts, err := record.New(s.Root).ListArtifacts(ctx)
	if err != nil {
		return workflow.GateEvidence{}, "", s.evidenceError(err)
	}
	names := map[string]bool{}
	for _, a := range artifacts {
		for _, related := range a.RelatedWorkItems {
			if related == wi.ID && a.Name != "" {
				names[a.Name] = true
			}
		}
	}
	byName := make([]string, 0, len(names))
	for n := range names {
		byName = append(byName, n)
	}
	sort.Strings(byName)
	comments, err := events.New(s.Root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: wi.ID},
		Type:    "comment",
	})
	if err != nil {
		return workflow.GateEvidence{}, "", s.evidenceError(err)
	}
	approvalID := ""
	matched, err := approval.MatchingGate(ctx, s.Root, wi.ID, stage, wi.Status)
	switch {
	case err == nil:
		approvalID = matched.ID
	case errors.Is(err, approval.ErrNotFound):
	default:
		return workflow.GateEvidence{}, "", s.evidenceError(err)
	}
	note := ""
	if approvalID == "" {
		// The usual reason a stage that needed an approval no longer has one:
		// 方案 §4.9 voided it when the work item left the status it was
		// requested from. Say so, or the operator sees only "approval
		// required" after having approved.
		if stale, serr := approval.InvalidatedFor(ctx, s.Root, wi.ID, stage); serr == nil {
			note = fmt.Sprintf(
				"%s was invalidated at %s when the work item left %q (方案 §4.9); request a new approval with `workloom approval request`",
				stale.ID, stale.InvalidatedAt.UTC().Format(time.RFC3339), stale.RequestedStatus)
		} else if !errors.Is(serr, approval.ErrNotFound) {
			return workflow.GateEvidence{}, "", s.evidenceError(serr)
		}
	}
	return workflow.GateEvidence{
		ArtifactNames: byName,
		CommentCount:  len(comments),
		ApprovalReady: approvalID != "",
		ApprovalNote:  note,
	}, approvalID, nil
}

func (s *Service) evidenceError(err error) error {
	if errors.Is(err, storage.ErrNotInitialized) {
		return Preconditionf("%v", err)
	}
	return Internalf("collect gate evidence: %v", err)
}

// checkTransitionGate refuses a transition whose target stage gate is not
// satisfied, listing every missing item. When the current policy file failed
// validation and the last-known-good snapshot took over, the returned notice
// carries that fact for the caller to surface.
//
// Known window: evidence (artifacts, comments) is collected before the
// transition transaction takes the project lock, so evidence removed in
// between is not re-checked. Gates are an application-level precondition,
// not a snapshot-isolated invariant.
func (s *Service) checkTransitionGate(ctx context.Context, wi *domain.WorkItem, target string) (string, string, error) {
	res, err := s.policyForWorkItem(ctx, wi)
	if err != nil {
		return "", "", s.errWorkflowPolicy(err)
	}
	if res.Policy == nil {
		return "", "", nil
	}
	notice := ""
	if res.Issue != nil {
		notice = fmt.Sprintf("workflow policy %q is invalid: %s; using %s", res.ID, res.Issue.String(), res.Source)
	}
	ev, approvalID, err := s.gateEvidence(ctx, wi, target)
	if err != nil {
		return "", "", err
	}
	gate := res.Policy.CheckGate(target, ev)
	if gate.Allowed {
		// Only a stage that declares require_approval may consume one:
		// otherwise a lingering approval would be burned by a transition
		// that never needed it (and ApprovalRequest's "nothing will consume
		// this approval" warning would be a lie).
		if declared, ok := res.Policy.Gates.Stages[target]; !ok || !declared.RequireApproval {
			approvalID = ""
		}
		return notice, approvalID, nil
	}
	return notice, "", errGate(res.Policy.File, target, gate.Missing)
}

// checkClaimQuality refuses a claim whose work item scores below the policy's
// quality threshold. While the current policy file is invalid, claims are
// blocked outright — the last-known-good snapshot serves read paths only
// (方案 §5.3: 配置错误只阻塞新任务派发).
func (s *Service) checkClaimQuality(ctx context.Context, wi *domain.WorkItem) error {
	res, err := s.policyForWorkItem(ctx, wi)
	if err != nil {
		return s.errWorkflowPolicy(err)
	}
	if res.Policy == nil {
		return nil
	}
	if res.Issue != nil {
		return s.errWorkflowPolicy(fmt.Errorf(
			"workflow policy %q is invalid: %s; claims stay blocked until the file is fixed (%s is retained for read paths)",
			res.ID, res.Issue.String(), res.Source))
	}
	quality := workflow.ScoreQuality(workflow.QualityInput{
		Title:              wi.Title,
		Description:        wi.Description,
		AcceptanceCriteria: wi.AcceptanceCriteria,
	})
	if !res.Policy.QualityGateBlocks(quality) {
		return nil
	}
	return errQuality(res.Policy.File, quality.Score, res.Policy.QualityGate.MinScore, quality.Improvements)
}

// errWorkflowPolicy reports a workflow policy that a work item declared but
// that cannot be used (missing or invalid). Callers refuse rather than
// silently skipping gates (fail closed; M3.5 adds last-known-good).
func (s *Service) errWorkflowPolicy(err error) error {
	return Invalidf(KindWorkflow, nil, "%v", err)
}

// errGate renders a stage gate rejection with one problem per unmet
// requirement, located at the policy that declared the gate.
func errGate(policyFile, stage string, missing []string) error {
	problems := make([]config.Problem, 0, len(missing))
	for _, m := range missing {
		problems = append(problems, config.Problem{File: policyFile, Field: "gates.stages." + stage, Reason: m})
	}
	word := "item"
	if len(missing) != 1 {
		word = "items"
	}
	return Invalidf(KindGate, problems,
		"stage gate for %q is not satisfied (%d missing %s)", stage, len(missing), word)
}

// errQuality renders a claim-time quality rejection with one problem per
// improvement item, located at the policy's threshold.
func errQuality(policyFile string, score, minScore int, improvements []string) error {
	problems := make([]config.Problem, 0, len(improvements))
	for _, m := range improvements {
		problems = append(problems, config.Problem{File: policyFile, Field: "quality_gate.min_score", Reason: m})
	}
	return Invalidf(KindQuality, problems,
		"quality gate not satisfied: score %d is below min_score %d", score, minScore)
}

// mapWorkflowError renders domain workflow refusals: the allowed candidates
// become located problems (实施计划 M3.6).
func (s *Service) mapWorkflowError(err error, policyFile string) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	var se *workitem.WorkflowStepError
	if errors.As(err, &se) {
		problems := make([]config.Problem, 0, len(se.Allowed))
		for _, c := range se.Allowed {
			reason := fmt.Sprintf("when %q satisfied", c.When)
			switch {
			case c.When == "":
				reason = "unconditional"
			case !c.Satisfied:
				reason = fmt.Sprintf("when %q not satisfied", c.When)
			}
			problems = append(problems, config.Problem{File: policyFile, Field: c.To, Reason: reason})
		}
		return Invalidf(KindWorkflow, problems, "%v", se)
	}
	return s.storeError(err)
}
