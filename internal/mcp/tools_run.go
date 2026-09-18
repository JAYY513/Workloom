package mcp

import (
	"context"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/domain"
)

// --- run_list / get / log ------------------------------------------------

type runListInput struct {
	WorkitemID string `json:"workitem_id,omitempty" jsonschema:"filter by work item id"`
	Status     string `json:"status,omitempty" jsonschema:"filter by run status"`
}

type runListResult struct {
	Runs  []*domain.Run `json:"runs"`
	Count int           `json:"count"`
}

func registerRunList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_list",
		Description: "List run records, optionally filtered by work item or status. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runListInput) (*mcpsdk.CallToolResult, runListResult, error) {
		runs, err := cfg.service().RunList(ctx, app.RunListRequest{WorkItemID: in.WorkitemID, Status: in.Status})
		if err != nil {
			return fail[runListResult](err)
		}
		if runs == nil {
			runs = []*domain.Run{}
		}
		return nil, runListResult{Runs: runs, Count: len(runs)}, nil
	})
}

type runGetInput struct {
	ID string `json:"id" jsonschema:"run id, e.g. run-20260918-1"`
}

func registerRunGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_get",
		Description: "Read one run with its version hash and context snapshot. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runGetInput) (*mcpsdk.CallToolResult, app.RunView, error) {
		view, err := cfg.service().RunGet(ctx, in.ID)
		if err != nil {
			return fail[app.RunView](err)
		}
		return nil, view, nil
	})
}

type runLogInput struct {
	ID    string `json:"id" jsonschema:"run id"`
	Limit int    `json:"limit,omitempty" jsonschema:"keep only the newest N events"`
}

func registerRunLog(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_log",
		Description: "Read a run's evidence trail: logs, commands, tests, changed files and the events recorded against it. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runLogInput) (*mcpsdk.CallToolResult, app.RunLogView, error) {
		view, err := cfg.service().RunLog(ctx, in.ID, in.Limit)
		if err != nil {
			return fail[app.RunLogView](err)
		}
		return nil, view, nil
	})
}

// --- run_create / update / heartbeat -------------------------------------

type runCreateInput struct {
	WorkitemID string `json:"workitem_id" jsonschema:"work item this run belongs to"`
	WorkflowID string `json:"workflow_id,omitempty" jsonschema:"workflow instance id (defaults to the work item's)"`
	Phase      string `json:"phase,omitempty" jsonschema:"initial phase label"`
	Actor      string `json:"actor" jsonschema:"operator identity"`
	Reason     string `json:"reason" jsonschema:"why the run exists"`
	AgentID    string `json:"agent_id,omitempty" jsonschema:"agent identity"`
	Harness    string `json:"harness,omitempty" jsonschema:"agent harness"`
	Model      string `json:"model,omitempty" jsonschema:"agent model"`
}

func registerRunCreate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_create",
		Description: "Record a standalone run for a work item (claim-created runs are the normal path and carry lease credentials).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runCreateInput) (*mcpsdk.CallToolResult, app.RunView, error) {
		view, err := cfg.service().RunCreate(ctx, app.CreateRunRequest{
			WorkItemID: in.WorkitemID, WorkflowID: in.WorkflowID, Phase: in.Phase,
			Actor: in.Actor, Reason: in.Reason,
			Agent: domain.RunAgent{ID: in.AgentID, Harness: in.Harness, Model: in.Model},
		})
		if err != nil {
			return fail[app.RunView](err)
		}
		return nil, view, nil
	})
}

type runUpdateInput struct {
	ID           string   `json:"id" jsonschema:"run id"`
	Expect       string   `json:"expect" jsonschema:"version hash from run_get (required: read before you write)"`
	Status       *string  `json:"status,omitempty" jsonschema:"new status"`
	Phase        *string  `json:"phase,omitempty" jsonschema:"new phase"`
	AddLogs      []string `json:"add_logs,omitempty" jsonschema:"log lines to append"`
	AddCommands  []string `json:"add_commands,omitempty" jsonschema:"commands to append"`
	AddTests     []string `json:"add_tests,omitempty" jsonschema:"tests to append"`
	ChangedFiles []string `json:"changed_files,omitempty" jsonschema:"replaces the changed files list"`
}

func registerRunUpdate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_update",
		Description: "Patch a run's evidence (logs, commands, tests, changed files) under the version guard.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runUpdateInput) (*mcpsdk.CallToolResult, app.RunView, error) {
		view, err := cfg.service().RunUpdate(ctx, app.UpdateRunRequest{
			ID: in.ID, Expect: in.Expect, Status: in.Status, Phase: in.Phase,
			AddLogs: in.AddLogs, AddCommands: in.AddCommands, AddTests: in.AddTests,
			ChangedFiles: in.ChangedFiles,
		})
		if err != nil {
			return fail[app.RunView](err)
		}
		return nil, view, nil
	})
}

type runHeartbeatInput struct {
	ID       string `json:"id" jsonschema:"run id"`
	Owner    string `json:"owner" jsonschema:"lease owner"`
	Token    string `json:"token" jsonschema:"lease token from the claim"`
	Actor    string `json:"actor" jsonschema:"operator identity"`
	Reason   string `json:"reason" jsonschema:"reason"`
	ExtendBy int    `json:"extend_by_seconds,omitempty" jsonschema:"lease extension in seconds (default: the claim duration)"`
}

func registerRunHeartbeat(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "run_heartbeat",
		Description: "Extend the lease of the run's work item (owner and token must match the active lease).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runHeartbeatInput) (*mcpsdk.CallToolResult, app.HeartbeatView, error) {
		var extendBy time.Duration
		if in.ExtendBy > 0 {
			extendBy = time.Duration(in.ExtendBy) * time.Second
		}
		view, err := cfg.service().RunHeartbeat(ctx, in.ID, in.Owner, in.Token, in.Actor, in.Reason, extendBy)
		if err != nil {
			return fail[app.HeartbeatView](err)
		}
		return nil, view, nil
	})
}

// --- run_complete / run_fail / run_cancel --------------------------------

type runFinishInput struct {
	ID     string `json:"id" jsonschema:"run id"`
	Expect string `json:"expect" jsonschema:"version hash from run_get (required: read before you write)"`
	Actor  string `json:"actor" jsonschema:"who decided the outcome"`
	Reason string `json:"reason" jsonschema:"why the attempt ended this way"`
	Note   string `json:"note,omitempty" jsonschema:"optional detail kept with the run's evidence"`
}

// registerRunFinish wires the three terminal transitions of §4.8. They only
// record an outcome — nothing here starts or stops a process — and each one
// refuses a run that already ended, so two actors cannot both claim it.
func registerRunFinish(s *mcpsdk.Server, cfg Config) {
	register := func(name, description, outcome string) {
		mcpsdk.AddTool(s, &mcpsdk.Tool{
			Name:        name,
			Description: description,
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runFinishInput) (*mcpsdk.CallToolResult, app.RunView, error) {
			view, err := cfg.service().RunFinish(ctx, app.RunFinishRequest{
				RunID: in.ID, Expect: in.Expect, Outcome: outcome,
				Actor: in.Actor, Reason: in.Reason, Note: in.Note,
			})
			if err != nil {
				return fail[app.RunView](err)
			}
			return nil, view, nil
		})
	}
	register("run_complete", "Mark an attempt as succeeded (§4.8). The completion check (claim head SHA) arrives with M6.6.", app.RunSucceeded)
	register("run_fail", "Mark an attempt as failed, with the reason recorded on the run.", app.RunFailed)
	register("run_cancel", "Mark an attempt as canceled (stopped before it decided its own outcome).", app.RunCanceled)
}
