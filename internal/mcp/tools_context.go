package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
)

// --- context_get / for_workitem / refresh / compact ----------------------

type contextGetInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"max references per list (default 5)"`
}

func registerContextGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "context_get",
		Description: "Assemble the project working context: facts, progress, recommended action and recent record references. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in contextGetInput) (*mcpsdk.CallToolResult, app.ContextView, error) {
		view, err := cfg.service().ContextGet(ctx, in.Limit)
		if err != nil {
			return fail[app.ContextView](err)
		}
		return nil, view, nil
	})
}

type contextForWorkitemInput struct {
	ID string `json:"id" jsonschema:"work item id"`
}

func registerContextForWorkitem(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "context_for_workitem",
		Description: "Assemble everything about one work item: the item, its workflow candidates, and the records and comments that reference it. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in contextForWorkitemInput) (*mcpsdk.CallToolResult, app.WorkitemContextView, error) {
		view, err := cfg.service().ContextForWorkitem(ctx, in.ID)
		if err != nil {
			return fail[app.WorkitemContextView](err)
		}
		return nil, view, nil
	})
}

type contextRefreshInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"max references per list (default 5)"`
}

func registerContextRefresh(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "context_refresh",
		Description: "Re-evaluate the working context, spelling out policy and readiness notices (last-known-good fallbacks included). Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in contextRefreshInput) (*mcpsdk.CallToolResult, app.ContextView, error) {
		view, err := cfg.service().ContextRefresh(ctx, in.Limit)
		if err != nil {
			return fail[app.ContextView](err)
		}
		return nil, view, nil
	})
}

type contextCompactInput struct{}

func registerContextCompact(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "context_compact",
		Description: "The smallest useful context: project identity, progress counts and the recommended action. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ contextCompactInput) (*mcpsdk.CallToolResult, app.ContextView, error) {
		view, err := cfg.service().ContextCompact(ctx)
		if err != nil {
			return fail[app.ContextView](err)
		}
		return nil, view, nil
	})
}

// --- knowledge_status ----------------------------------------------------

type knowledgeStatusInput struct{}

func registerKnowledgeStatus(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "knowledge_status",
		Description: "Report the knowledge layer's availability. The index layer arrives with M5 (方案 §12.6); without it the answer is an explicit degradation, not an error.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ knowledgeStatusInput) (*mcpsdk.CallToolResult, app.KnowledgeStatusView, error) {
		view, err := cfg.service().KnowledgeStatus(ctx)
		if err != nil {
			return fail[app.KnowledgeStatusView](err)
		}
		return nil, view, nil
	})
}

type knowledgeValidateInput struct {
	Paths []string `json:"paths,omitempty" jsonschema:"page roots: project-relative directories or .md files (default: the configured page roots)"`
}

func registerKnowledgeValidate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "knowledge_validate",
		Description: "Validate the knowledge page layer's front matter contract (status/type/triggers/description/source_commit). Errors are located and make the call fail; warnings are advisory. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in knowledgeValidateInput) (*mcpsdk.CallToolResult, app.KnowledgeValidateView, error) {
		view, err := cfg.service().KnowledgeValidate(ctx, in.Paths)
		if err != nil {
			return fail[app.KnowledgeValidateView](err)
		}
		return nil, view, nil
	})
}
