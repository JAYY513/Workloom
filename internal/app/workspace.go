package app

import (
	"context"
	"fmt"

	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/run"
	"github.com/JAYY513/Workloom/internal/workflow"
	"github.com/JAYY513/Workloom/internal/workspace"
)

// WorkspacePrepareRequest asks for the execution workspace of a work item
// (方案 §4.8): create it if missing, reuse it if it exists, and bind it to a run
// when one is named.
type WorkspacePrepareRequest struct {
	WorkItemID string
	RunID      string
	Branch     string
	Actor      string
	Reason     string
}

// WorkspaceView is what preparation produced.
type WorkspaceView struct {
	WorkItemID string                `json:"workitem_id"`
	RunID      string                `json:"run_id,omitempty"`
	Key        string                `json:"key"`
	Path       string                `json:"path"`
	Branch     string                `json:"branch"`
	Created    bool                  `json:"created"`
	Hook       *workspace.HookReport `json:"hook,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
}

// WorkspacePrepare ensures the work item's workspace exists and, when a run is
// named, binds it to that run under the version guard. A workspace that exists
// is reused without running after_create again; a broken policy blocks the
// dispatch (the same fail-closed rule claim uses), while a non-fatal hook
// failure is reported as a warning.
func (s *Service) WorkspacePrepare(ctx context.Context, req WorkspacePrepareRequest) (WorkspaceView, error) {
	if req.WorkItemID == "" || req.Actor == "" || req.Reason == "" {
		return WorkspaceView{}, Usagef("workspace prepare requires workitem, actor and reason")
	}
	md, err := s.project()
	if err != nil {
		return WorkspaceView{}, err
	}
	wi, err := s.WorkitemGet(ctx, req.WorkItemID)
	if err != nil {
		return WorkspaceView{}, err
	}
	res, err := s.policyForWorkItem(ctx, wi.Item)
	if err != nil {
		return WorkspaceView{}, s.errWorkflowPolicy(err)
	}
	if res.Policy != nil && res.Issue != nil {
		return WorkspaceView{}, s.errWorkflowPolicy(fmt.Errorf(
			"workflow policy %q is invalid: %s; workspace creation stays blocked until the file is fixed",
			res.ID, res.Issue.String()))
	}
	var hooks map[string]workspace.Hook
	if res.Policy != nil {
		hooks = workspaceHooks(res.Policy)
	}
	configured := ""
	if md.Config != nil {
		configured = md.Config.WorkspaceRoot
	}
	ws, err := workspace.Ensure(ctx, workspace.EnsureOptions{
		ProjectRoot: s.Root, Root: configured,
		Identifier: wi.Item.ID, Branch: req.Branch, Hooks: hooks,
	})
	if err != nil {
		return WorkspaceView{}, Preconditionf("%v", err)
	}

	view := WorkspaceView{
		WorkItemID: wi.Item.ID, RunID: req.RunID,
		Key: ws.Key, Path: ws.Path, Branch: ws.Branch, Created: ws.Created, Hook: ws.Hook,
	}
	if req.RunID != "" {
		if err := s.bindWorkspaceToRun(ctx, req.RunID, ws); err != nil {
			return WorkspaceView{}, err
		}
	}
	eventType := "workspace_reused"
	if ws.Created {
		eventType = "workspace_created"
	}
	subject := domain.Reference{Type: "workitem", ID: wi.Item.ID}
	if req.RunID != "" {
		subject = domain.Reference{Type: "run", ID: req.RunID}
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      eventType,
		Subject:   subject,
		ProjectID: wi.Item.ProjectID,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: wi.Item.ID}},
		Content:   fmt.Sprintf("%s (%s) on branch %s", ws.Path, req.Reason, ws.Branch),
		Time:      s.now(),
	}); err != nil {
		return WorkspaceView{}, s.storeError(err)
	}
	return view, nil
}

// bindWorkspaceToRun records the workspace on the run under the version guard,
// so the attempt's own record says where it ran, and records the head it starts
// from — the claim head the completion check compares against (§4.8). Directory
// workspaces have no branch and no HEAD: an empty claim head is recorded
// rather than invented. A git worktree whose head cannot be read is an error:
// without it the attempt could never be verified.
func (s *Service) bindWorkspaceToRun(ctx context.Context, runID string, ws workspace.Workspace) error {
	head := ""
	if ws.Branch != "" {
		var err error
		head, err = workspace.HeadSHA(ws.Path)
		if err != nil {
			return Preconditionf("read the workspace head of %s: %v", ws.Path, err)
		}
	}
	r, raw, err := readRun(ctx, s, runID)
	if err != nil {
		return err
	}
	r.Workspace = domain.Workspace{Path: ws.Path, Branch: ws.Branch, Worktree: ws.Key}
	r.Claim.HeadSHA = head
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}

// workspaceHooks maps the policy's hook declarations onto the workspace
// package's view of them.
func workspaceHooks(policy *workflow.Policy) map[string]workspace.Hook {
	if policy == nil || len(policy.Hooks) == 0 {
		return nil
	}
	out := make(map[string]workspace.Hook, len(policy.Hooks))
	for name, hook := range policy.Hooks {
		out[name] = workspace.Hook{Command: hook.Command, TimeoutSeconds: hook.TimeoutSeconds}
	}
	return out
}

// WorkspaceRemoveRequest takes a workspace down.
type WorkspaceRemoveRequest struct {
	WorkItemID string
	Path       string
	Force      bool
	Actor      string
	Reason     string
}

// WorkspaceRemove takes the workspace down: the before_remove hook first (its
// failure is recorded, never fatal), then the worktree registration and the
// directory. The branch is kept — it may hold the run's commits.
func (s *Service) WorkspaceRemove(ctx context.Context, req WorkspaceRemoveRequest) (workspace.RemoveReport, error) {
	if req.Actor == "" || req.Reason == "" {
		return workspace.RemoveReport{}, Usagef("workspace remove requires actor and reason")
	}
	if req.WorkItemID == "" && req.Path == "" {
		return workspace.RemoveReport{}, Usagef("workspace remove requires a work item or a path")
	}
	md, err := s.project()
	if err != nil {
		return workspace.RemoveReport{}, err
	}
	hooks := map[string]workspace.Hook{}
	policyWarning := ""
	workItemID := req.WorkItemID
	if workItemID != "" {
		wi, err := s.WorkitemGet(ctx, workItemID)
		if err != nil {
			return workspace.RemoveReport{}, err
		}
		res, err := s.policyForWorkItem(ctx, wi.Item)
		if err != nil {
			return workspace.RemoveReport{}, s.errWorkflowPolicy(err)
		}
		if res.Policy != nil {
			// Removal is a cleanup path: a broken policy must not trap a
			// workspace, so the last-known-good hooks are used and the
			// failure is reported as a warning.
			hooks = workspaceHooks(res.Policy)
			if res.Issue != nil {
				policyWarning = fmt.Sprintf("workflow policy %q is invalid: %s; removal uses %s",
					res.ID, res.Issue.String(), res.Source)
			}
		}
	}
	configured := ""
	if md.Config != nil {
		configured = md.Config.WorkspaceRoot
	}
	rep, err := workspace.Remove(ctx, workspace.RemoveOptions{
		ProjectRoot: s.Root, Root: configured,
		Path: req.Path, Identifier: req.WorkItemID, Force: req.Force, Hooks: hooks,
	})
	if policyWarning != "" {
		rep.Warnings = append(rep.Warnings, policyWarning)
	}
	if err != nil {
		// A dirty worktree without --force, an escaping path or a git
		// failure are all preconditions the caller can fix.
		return rep, Preconditionf("%v", err)
	}
	if rep.Removed {
		subject := domain.Reference{Type: "workitem", ID: workItemID}
		if workItemID == "" {
			subject = domain.Reference{Type: "workspace", ID: rep.Path}
		}
		if err := events.New(s.Root).Append(ctx, &domain.Event{
			Type:      "workspace_removed",
			Subject:   subject,
			ProjectID: projectID(md),
			Actor:     req.Actor,
			Content:   fmt.Sprintf("%s (%s)", rep.Path, req.Reason),
			Time:      s.now(),
		}); err != nil {
			return rep, s.storeError(err)
		}
	}
	return rep, nil
}

// WorkspaceList reports the workspaces below the root (read-only).
func (s *Service) WorkspaceList(ctx context.Context, workItemID string) ([]workspace.Listing, error) {
	md, err := s.project()
	if err != nil {
		return nil, err
	}
	if workItemID != "" {
		if _, err := s.WorkitemGet(ctx, workItemID); err != nil {
			return nil, err
		}
	}
	configured := ""
	if md.Config != nil {
		configured = md.Config.WorkspaceRoot
	}
	items, err := workspace.List(s.Root, configured)
	if err != nil {
		return nil, Preconditionf("%v", err)
	}
	if workItemID == "" {
		return items, nil
	}
	key := workspace.Key(workItemID)
	filtered := make([]workspace.Listing, 0, 1)
	for _, item := range items {
		if item.Key == key {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func projectID(md *config.Metadata) string {
	if md == nil || md.Project == nil {
		return ""
	}
	return md.Project.ID
}
