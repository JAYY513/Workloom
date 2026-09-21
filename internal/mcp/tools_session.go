package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/app"
)

// agentSessionStartInput is the §8.3 session request. The harness's identity
// is reported back in the response, never recorded: starting a session must
// not change project state.
type agentSessionStartInput struct {
	Harness   string `json:"harness,omitempty" jsonschema:"harness name, e.g. codex"`
	AgentID   string `json:"agent_id,omitempty" jsonschema:"agent identity within the harness"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace path the agent will work in"`
	Intent    string `json:"intent,omitempty" jsonschema:"what the agent intends to do this session"`
	Compact   bool   `json:"compact,omitempty" jsonschema:"trim the context payload for small windows"`
}

func registerAgentSessionStart(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "agent_session_start",
		Description: "Start a session in one call: project facts, current state, work in flight, the single recommended next action and the context behind it. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in agentSessionStartInput) (*mcpsdk.CallToolResult, app.SessionView, error) {
		view, err := cfg.service().SessionStart(ctx, app.SessionRequest{
			Harness: in.Harness, AgentID: in.AgentID, Workspace: in.Workspace,
			Intent: in.Intent, Compact: in.Compact,
		})
		if err != nil {
			return fail[app.SessionView](err)
		}
		return nil, view, nil
	})
}
