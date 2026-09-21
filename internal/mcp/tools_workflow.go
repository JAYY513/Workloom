package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/app"
)

// --- workflow_list -------------------------------------------------------

type workflowListInput struct{}

type workflowListResult struct {
	Policies []app.PolicySummary `json:"policies"`
	Warnings any                 `json:"warnings,omitempty"`
}

func registerWorkflowList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workflow_list",
		Description: "List the workflow policy files with their identity; located issues come back as problems.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ workflowListInput) (*mcpsdk.CallToolResult, workflowListResult, error) {
		policies, warnings, err := cfg.service().WorkflowList(ctx)
		if err != nil {
			return fail[workflowListResult](err)
		}
		if policies == nil {
			policies = []app.PolicySummary{}
		}
		return nil, workflowListResult{Policies: policies, Warnings: warnings}, nil
	})
}

// --- workflow_get --------------------------------------------------------

type workflowGetInput struct {
	ID string `json:"id" jsonschema:"work item id"`
}

func registerWorkflowGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workflow_get",
		Description: "Read a work item's workflow instance with the evaluated step candidates.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workflowGetInput) (*mcpsdk.CallToolResult, app.WorkflowView, error) {
		view, err := cfg.service().WorkflowGet(ctx, in.ID)
		if err != nil {
			return fail[app.WorkflowView](err)
		}
		return nil, view, nil
	})
}

// --- workflow_start ------------------------------------------------------

type workflowStartInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Policy string `json:"policy" jsonschema:"workflow policy id"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"start reason"`
	Owner  string `json:"owner,omitempty" jsonschema:"lease owner when the work item is claimed"`
	Token  string `json:"token,omitempty" jsonschema:"lease token when the work item is claimed"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkflowStart(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workflow_start",
		Description: "Attach and start a workflow instance on a work item. Dispatch-like: the policy file must be currently valid.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workflowStartInput) (*mcpsdk.CallToolResult, app.WorkflowView, error) {
		view, err := cfg.service().WorkflowStart(ctx, in.ID, in.Policy, in.Actor, in.Reason, in.Owner, in.Token, in.Expect)
		if err != nil {
			return fail[app.WorkflowView](err)
		}
		return nil, view, nil
	})
}

// --- workflow_step_next --------------------------------------------------

type workflowStepNextInput struct {
	ID string `json:"id" jsonschema:"work item id"`
}

func registerWorkflowStepNext(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workflow_step_next",
		Description: "Evaluate the current step's candidates with their conditions.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workflowStepNextInput) (*mcpsdk.CallToolResult, app.WorkflowView, error) {
		view, err := cfg.service().WorkflowStepNext(ctx, in.ID)
		if err != nil {
			return fail[app.WorkflowView](err)
		}
		return nil, view, nil
	})
}

// --- workflow_step_complete ---------------------------------------------

type workflowStepCompleteInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	To     string `json:"to,omitempty" jsonschema:"target step id (must be a declared candidate)"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"reason"`
	Owner  string `json:"owner,omitempty" jsonschema:"lease owner when the work item is claimed"`
	Token  string `json:"token,omitempty" jsonschema:"lease token when the work item is claimed"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkflowStepComplete(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "workflow_step_complete",
		Description: "Advance the instance (declaration order unless a target step is named). Refusals list every allowed next step.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in workflowStepCompleteInput) (*mcpsdk.CallToolResult, app.WorkflowView, error) {
		view, err := cfg.service().WorkflowStepComplete(ctx, in.ID, in.To, in.Actor, in.Reason, in.Owner, in.Token, in.Expect)
		if err != nil {
			return failNotice[app.WorkflowView](err, view.Notice)
		}
		return nil, view, nil
	})
}

// --- workflow_pause / resume / cancel ------------------------------------

type workflowSignalInput struct {
	ID     string `json:"id" jsonschema:"work item id"`
	Actor  string `json:"actor" jsonschema:"operator identity"`
	Reason string `json:"reason" jsonschema:"reason"`
	Owner  string `json:"owner,omitempty" jsonschema:"lease owner when the work item is claimed"`
	Token  string `json:"token,omitempty" jsonschema:"lease token when the work item is claimed"`
	Expect string `json:"expect" jsonschema:"version hash from workitem_get (required: read before you write)"`
}

func registerWorkflowPause(s *mcpsdk.Server, cfg Config) {
	registerWorkflowSignal(s, cfg, "workflow_pause", "pause",
		"Hold the instance: a paused instance refuses step advancement.")
}

func registerWorkflowResume(s *mcpsdk.Server, cfg Config) {
	registerWorkflowSignal(s, cfg, "workflow_resume", "resume", "Release a paused instance.")
}

func registerWorkflowCancel(s *mcpsdk.Server, cfg Config) {
	registerWorkflowSignal(s, cfg, "workflow_cancel", "cancel", "Cancel the instance (terminal).")
}

func registerWorkflowSignal(s *mcpsdk.Server, cfg Config, name, action, description string) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: description},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, in workflowSignalInput) (*mcpsdk.CallToolResult, app.WorkflowView, error) {
			view, err := cfg.service().WorkflowSignal(ctx, action, in.ID, in.Actor, in.Reason, in.Owner, in.Token, in.Expect)
			if err != nil {
				return fail[app.WorkflowView](err)
			}
			return nil, view, nil
		})
}
