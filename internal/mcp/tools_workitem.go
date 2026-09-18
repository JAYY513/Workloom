package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/domain"
)

// --- workitem_list -------------------------------------------------------

type workitemListInput struct {
	Status string `json:"status,omitempty" jsonschema:"filter by status (draft, backlog, ready, in_progress, blocked, review, verification, done, cancelled)"`
	Type   string `json:"type,omitempty" jsonschema:"filter by work item type"`
	Parent string `json:"parent,omitempty" jsonschema:"filter by parent work item id"`
}

type workitemListResult struct {
	WorkItems []*domain.WorkItem `json:"workitems"`
	Count     int                `json:"count"`
}

func registerWorkitemList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_list",
		Description: "List work items, optionally filtered by status, type or parent.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemListInput) (*mcpsdk.CallToolResult, workitemListResult, error) {
		items, err := cfg.service().WorkitemList(ctx, app.WorkitemFilter{Status: in.Status, Type: in.Type, Parent: in.Parent})
		if err != nil {
			return fail[workitemListResult](err)
		}
		if items == nil {
			items = []*domain.WorkItem{}
		}
		return nil, workitemListResult{WorkItems: items, Count: len(items)}, nil
	})
}

// --- workitem_get --------------------------------------------------------

type workitemGetInput struct {
	ID string `json:"id" jsonschema:"work item id, e.g. WLM-1"`
}

func registerWorkitemGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_get",
		Description: "Read one work item with its version hash (the guard every write tool consumes).",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemGetInput) (*mcpsdk.CallToolResult, app.WorkItemView, error) {
		view, err := cfg.service().WorkitemGet(ctx, in.ID)
		if err != nil {
			return fail[app.WorkItemView](err)
		}
		return nil, view, nil
	})
}

// --- workitem_next -------------------------------------------------------

type workitemNextInput struct{}

type workitemNextResult struct {
	Verdict   string             `json:"verdict"`
	Reasons   []string           `json:"reasons"`
	Risks     any                `json:"risks"`
	Fixes     any                `json:"fixes,omitempty"`
	Next      any                `json:"next"`
	WorkItems []*domain.WorkItem `json:"workitems,omitempty"`
}

func registerWorkitemNext(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_next",
		Description: "Evaluate project readiness (PASS/CONCERNS/FAIL), list risks and return the single recommended next action (方案 §7.4).",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ workitemNextInput) (*mcpsdk.CallToolResult, workitemNextResult, error) {
		report, items, err := cfg.service().Next(ctx)
		if err != nil {
			return fail[workitemNextResult](err)
		}
		return nil, workitemNextResult{
			Verdict: report.Verdict, Reasons: report.Reasons, Risks: report.Risks,
			Fixes: report.Fixes, Next: report.Next, WorkItems: items,
		}, nil
	})
}

// --- workitem_create -----------------------------------------------------

type workitemCreateInput struct {
	Title       string `json:"title" jsonschema:"work item title"`
	Description string `json:"description,omitempty" jsonschema:"what needs to be done"`
	Type        string `json:"type,omitempty" jsonschema:"work item type (default task)"`
	ParentID    string `json:"parent_id,omitempty" jsonschema:"parent work item id"`
	Priority    int    `json:"priority,omitempty" jsonschema:"numeric priority, higher first"`
	Prefix      string `json:"prefix,omitempty" jsonschema:"ID prefix (default WLM)"`
	Actor       string `json:"actor" jsonschema:"operator identity"`
	Reason      string `json:"reason" jsonschema:"why the work item exists"`
}

func registerWorkitemCreate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_create",
		Description: "Create a draft work item. Requires actor and reason for the audit trail.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemCreateInput) (*mcpsdk.CallToolResult, app.WorkItemView, error) {
		view, err := cfg.service().WorkitemCreate(ctx, app.CreateWorkitemRequest{
			Title: in.Title, Description: in.Description, Type: in.Type, Prefix: in.Prefix,
			ParentID: in.ParentID, Priority: in.Priority, Actor: in.Actor, Reason: in.Reason,
		})
		if err != nil {
			return fail[app.WorkItemView](err)
		}
		return nil, view, nil
	})
}

// --- workitem_update -----------------------------------------------------

type workitemUpdateInput struct {
	ID                 string   `json:"id" jsonschema:"work item id"`
	Expect             string   `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
	Title              *string  `json:"title,omitempty" jsonschema:"new title"`
	Description        *string  `json:"description,omitempty" jsonschema:"new description"`
	Priority           *int     `json:"priority,omitempty" jsonschema:"new priority"`
	AssignedAgent      *string  `json:"assigned_agent,omitempty" jsonschema:"agent identity to assign (empty clears)"`
	AssignedHarness    *string  `json:"assigned_harness,omitempty" jsonschema:"harness adapter dispatch should drive (empty clears)"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty" jsonschema:"replaces the acceptance criteria list"`
	Dependencies       []string `json:"dependencies,omitempty" jsonschema:"replaces the dependency list"`
	Constraints        []string `json:"constraints,omitempty" jsonschema:"replaces the constraints list"`
}

func registerWorkitemUpdate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_update",
		Description: "Patch mutable work item fields under the version guard (expect from workitem_get).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemUpdateInput) (*mcpsdk.CallToolResult, app.WorkItemView, error) {
		view, err := cfg.service().WorkitemUpdate(ctx, in.ID, app.UpdateWorkitemRequest{
			Title: in.Title, Description: in.Description, Priority: in.Priority,
			AcceptanceCriteria: in.AcceptanceCriteria, Dependencies: in.Dependencies,
			Constraints: in.Constraints, Expect: in.Expect,
			AssignedAgent: in.AssignedAgent, AssignedHarness: in.AssignedHarness,
		})
		if err != nil {
			return fail[app.WorkItemView](err)
		}
		return nil, view, nil
	})
}

// --- workitem_transition -------------------------------------------------

type workitemTransitionInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	To     string `json:"to" jsonschema:"target status"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"transition reason"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

type transitionResult struct {
	app.WorkItemView
	Notice string `json:"notice,omitempty"`
}

func registerWorkitemTransition(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_transition",
		Description: "Advance the state machine. Stage gates run first and the matching approval (if any) is consumed in the same transaction; refusals list every missing gate item.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemTransitionInput) (*mcpsdk.CallToolResult, transitionResult, error) {
		view, notice, err := cfg.service().WorkitemTransition(ctx, in.ID, in.To, in.Actor, in.Reason, in.Expect)
		if err != nil {
			return failNotice[transitionResult](err, notice)
		}
		return nil, transitionResult{WorkItemView: view, Notice: notice}, nil
	})
}

// --- workitem_claim ------------------------------------------------------

type workitemClaimInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Owner  string `json:"owner" jsonschema:"claimer identity (also the lease owner)"`
	Reason string `json:"reason" jsonschema:"claim reason"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkitemClaim(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_claim",
		Description: "Claim a ready work item: the claim quality gate runs first, then the claim creates the run and the lease in one transaction.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemClaimInput) (*mcpsdk.CallToolResult, app.ClaimView, error) {
		res, err := cfg.service().WorkitemClaim(ctx, in.ID, in.Owner, in.Reason, in.Expect)
		if err != nil {
			return fail[app.ClaimView](err)
		}
		return nil, res, nil
	})
}

// --- workitem_release / start -------------------------------------------

type workitemLeaseInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Owner  string `json:"owner" jsonschema:"lease owner"`
	Token  string `json:"token" jsonschema:"lease token from the claim"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"reason"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkitemRelease(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_release",
		Description: "Release an active lease (owner and token must match the lease).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemLeaseInput) (*mcpsdk.CallToolResult, app.WorkItemView, error) {
		view, err := cfg.service().WorkitemRelease(ctx, in.ID, in.Owner, in.Token, in.Actor, in.Reason, in.Expect)
		if err != nil {
			return fail[app.WorkItemView](err)
		}
		return nil, view, nil
	})
}

func registerWorkitemStart(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_start",
		Description: "Move a claimed work item into its run (owner and token must match the active lease).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemLeaseInput) (*mcpsdk.CallToolResult, app.ClaimView, error) {
		res, err := cfg.service().WorkitemStart(ctx, in.ID, in.Owner, in.Token, in.Actor, in.Reason, in.Expect)
		if err != nil {
			return fail[app.ClaimView](err)
		}
		return nil, res, nil
	})
}

// --- workitem_block / complete ------------------------------------------

type workitemSignalInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"reason"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkitemBlock(s *mcpsdk.Server, cfg Config) {
	registerWorkitemTarget(s, cfg, "workitem_block", domain.StatusBlocked,
		"Move a work item to blocked (gate-checked transition; blocked work items never enter the next-action candidates).")
}

func registerWorkitemComplete(s *mcpsdk.Server, cfg Config) {
	registerWorkitemTarget(s, cfg, "workitem_complete", domain.StatusDone,
		"Move a work item to done. The completion gate runs first, and related decision/finding records propagate to the parent and siblings in the same transaction.")
}

func registerWorkitemTarget(s *mcpsdk.Server, cfg Config, name, target, description string) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: description},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemSignalInput) (*mcpsdk.CallToolResult, transitionResult, error) {
			view, notice, err := cfg.service().WorkitemTransition(ctx, in.ID, target, in.Actor, in.Reason, in.Expect)
			if err != nil {
				return failNotice[transitionResult](err, notice)
			}
			return nil, transitionResult{WorkItemView: view, Notice: notice}, nil
		})
}

// --- workitem_comment ----------------------------------------------------

type workitemCommentInput struct {
	ID      string `json:"id" jsonschema:"work item id"`
	Text    string `json:"text" jsonschema:"comment text"`
	Actor   string `json:"actor" jsonschema:"author identity"`
	ReplyTo string `json:"reply_to,omitempty" jsonschema:"event id this comment replies to"`
}

type commentResult struct {
	Event *domain.Event `json:"event"`
}

func registerWorkitemComment(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workitem_comment",
		Description: "Append a comment event to a work item (comments count as gate evidence for comment gates).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workitemCommentInput) (*mcpsdk.CallToolResult, commentResult, error) {
		view, err := cfg.service().WorkitemComment(ctx, in.ID, in.Text, in.Actor, in.ReplyTo)
		if err != nil {
			return fail[commentResult](err)
		}
		return nil, commentResult{Event: view.Event}, nil
	})
}

// --- workitem_add_dependency / remove ------------------------------------

type dependencyInput struct {
	ID        string `json:"id" jsonschema:"work item id"`
	DependsOn string `json:"depends_on" jsonschema:"the work item this one depends on"`
	Expect    string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkitemAddDependency(s *mcpsdk.Server, cfg Config) {
	registerDependency(s, cfg, "workitem_add_dependency",
		"Record that a work item depends on another (self-dependencies and duplicates are refused).", true)
}

func registerWorkitemRemoveDependency(s *mcpsdk.Server, cfg Config) {
	registerDependency(s, cfg, "workitem_remove_dependency", "Drop a recorded dependency.", false)
}

func registerDependency(s *mcpsdk.Server, cfg Config, name, description string, add bool) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: description},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, in dependencyInput) (*mcpsdk.CallToolResult, app.WorkItemView, error) {
			var (
				view app.WorkItemView
				err  error
			)
			if add {
				view, err = cfg.service().WorkitemAddDependency(ctx, in.ID, in.DependsOn, in.Expect)
			} else {
				view, err = cfg.service().WorkitemRemoveDependency(ctx, in.ID, in.DependsOn, in.Expect)
			}
			if err != nil {
				return fail[app.WorkItemView](err)
			}
			return nil, view, nil
		})
}
