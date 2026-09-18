package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/domain"
)

// --- approval_list -------------------------------------------------------

type approvalListInput struct {
	WorkItemID string `json:"workitem_id,omitempty" jsonschema:"filter by work item id"`
	Status     string `json:"status,omitempty" jsonschema:"filter by status (pending, approved, rejected)"`
}

type approvalListResult struct {
	Approvals []*domain.Approval `json:"approvals"`
}

func registerApprovalList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "approval_list",
		Description: "List approvals, optionally filtered by work item or status.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in approvalListInput) (*mcpsdk.CallToolResult, approvalListResult, error) {
		approvals, err := cfg.service().ApprovalList(ctx, app.ApprovalFilter{WorkItemID: in.WorkItemID, Status: in.Status})
		if err != nil {
			return fail[approvalListResult](err)
		}
		return nil, approvalListResult{Approvals: approvals}, nil
	})
}

// --- approval_get --------------------------------------------------------

type approvalGetInput struct {
	ID string `json:"id" jsonschema:"approval id, e.g. approval-1"`
}

type approvalResult struct {
	Approval *domain.Approval `json:"approval"`
}

func registerApprovalGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "approval_get",
		Description: "Read one approval.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in approvalGetInput) (*mcpsdk.CallToolResult, approvalResult, error) {
		apr, err := cfg.service().ApprovalGet(ctx, in.ID)
		if err != nil {
			return fail[approvalResult](err)
		}
		return nil, approvalResult{Approval: apr}, nil
	})
}

// --- approval_request ----------------------------------------------------

type approvalRequestInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Stage  string `json:"stage" jsonschema:"gate stage the approval targets (the target status)"`
	Scope  string `json:"scope,omitempty" jsonschema:"stage_gate (default) or action"`
	RunID  string `json:"run_id,omitempty" jsonschema:"run that triggered the request"`
	Actor  string `json:"actor" jsonschema:"requester identity"`
	Reason string `json:"reason" jsonschema:"why the approval is needed"`
}

type approvalRequestResult struct {
	Approval *domain.Approval `json:"approval"`
	Warning  string           `json:"warning,omitempty"`
}

func registerApprovalRequest(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "approval_request",
		Description: "Record a pending approval. A stage-gate approval is consumed by the gate-checked transition it authorizes; leaving the requested status invalidates it.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in approvalRequestInput) (*mcpsdk.CallToolResult, approvalRequestResult, error) {
		apr, warning, err := cfg.service().ApprovalRequest(ctx, app.RequestApprovalRequest{
			WorkItemID: in.ID, Stage: in.Stage, Scope: in.Scope, RunID: in.RunID, Actor: in.Actor, Reason: in.Reason,
		})
		if err != nil {
			return fail[approvalRequestResult](err)
		}
		return nil, approvalRequestResult{Approval: apr, Warning: warning}, nil
	})
}

// --- approval_decide -----------------------------------------------------

type approvalDecideInput struct {
	ID       string `json:"id" jsonschema:"approval id"`
	Decision string `json:"decision" jsonschema:"approve or reject"`
	By       string `json:"by" jsonschema:"decider identity"`
	Comment  string `json:"comment,omitempty" jsonschema:"decision comment (approve)"`
	Reason   string `json:"reason,omitempty" jsonschema:"rejection reason (required for reject)"`
}

type approvalDecideResult struct {
	Approval    *domain.Approval `json:"approval"`
	Disposition string           `json:"disposition,omitempty"`
}

func registerApprovalDecide(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "approval_decide",
		Description: "Approve or reject an approval. A stage-gate rejection commits the decision and the work item disposition (blocked, or the policy's on_reject target) in one transaction.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in approvalDecideInput) (*mcpsdk.CallToolResult, approvalDecideResult, error) {
		svc := cfg.service()
		switch in.Decision {
		case "approve":
			apr, err := svc.ApprovalApprove(ctx, app.DecideApprovalRequest{ID: in.ID, By: in.By, Comment: in.Comment})
			if err != nil {
				return fail[approvalDecideResult](err)
			}
			return nil, approvalDecideResult{Approval: apr}, nil
		case "reject":
			apr, disposition, err := svc.ApprovalReject(ctx, app.DecideApprovalRequest{ID: in.ID, By: in.By, Reason: in.Reason})
			if err != nil {
				return fail[approvalDecideResult](err)
			}
			return nil, approvalDecideResult{Approval: apr, Disposition: disposition}, nil
		default:
			return usageFail[approvalDecideResult]("decision must be approve or reject (got %q)", in.Decision)
		}
	})
}
