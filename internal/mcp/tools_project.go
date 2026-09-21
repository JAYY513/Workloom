package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/app"
	"github.com/JAYY513/Workloom/internal/domain"
)

// --- project_get ---------------------------------------------------------

type projectGetInput struct{}

func registerProjectGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_get",
		Description: "Read the project metadata with its version hash.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ projectGetInput) (*mcpsdk.CallToolResult, app.ProjectView, error) {
		view, err := cfg.service().ProjectGet(ctx)
		if err != nil {
			return fail[app.ProjectView](err)
		}
		return nil, view, nil
	})
}

// --- project_status ------------------------------------------------------

type projectStatusInput struct{}

func registerProjectStatus(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_status",
		Description: "Project metadata, work item counts by status, the readiness verdict and risks.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ projectStatusInput) (*mcpsdk.CallToolResult, app.ProjectStatusView, error) {
		view, err := cfg.service().ProjectStatus(ctx)
		if err != nil {
			return fail[app.ProjectStatusView](err)
		}
		return nil, view, nil
	})
}

// --- project_blueprint_get ----------------------------------------------

type projectBlueprintInput struct{}

type blueprintResult struct {
	Artifact *domain.Artifact `json:"artifact"`
}

func registerProjectBlueprint(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_blueprint_get",
		Description: "Read the artifact the project declares as its blueprint.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ projectBlueprintInput) (*mcpsdk.CallToolResult, blueprintResult, error) {
		art, err := cfg.service().ProjectBlueprint(ctx)
		if err != nil {
			return fail[blueprintResult](err)
		}
		return nil, blueprintResult{Artifact: art}, nil
	})
}

// --- project_list --------------------------------------------------------

type projectListInput struct{}

type projectListResult struct {
	Projects []app.ProjectEntry `json:"projects"`
}

func registerProjectList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_list",
		Description: "List the user-level project registry (paths and identity only).",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ projectListInput) (*mcpsdk.CallToolResult, projectListResult, error) {
		entries, err := cfg.service().ProjectList(ctx)
		if err != nil {
			return fail[projectListResult](err)
		}
		return nil, projectListResult{Projects: entries}, nil
	})
}

// --- project_create ------------------------------------------------------

type projectCreateInput struct {
	Path string `json:"path,omitempty" jsonschema:"absolute path to an existing directory (default: the current project root)"`
}

type projectCreateResult struct {
	Root         string   `json:"root"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Created      []string `json:"created"`
	RegistryPath string   `json:"registry_path"`
}

func registerProjectCreate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_create",
		Description: "Initialize .devsys/ in an existing directory (default: the server's project root) and register it. Admin operation.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in projectCreateInput) (*mcpsdk.CallToolResult, projectCreateResult, error) {
		res, regPath, err := cfg.service().ProjectCreate(ctx, in.Path)
		if err != nil {
			return fail[projectCreateResult](err)
		}
		created := res.Created
		if created == nil {
			created = []string{}
		}
		return nil, projectCreateResult{
			Root: res.Root, ID: res.ID, Name: res.Name, Created: created, RegistryPath: regPath,
		}, nil
	})
}

// --- project_update ------------------------------------------------------

type projectUpdateInput struct {
	Name                *string `json:"name,omitempty" jsonschema:"new project name"`
	Description         *string `json:"description,omitempty" jsonschema:"new description"`
	Status              *string `json:"status,omitempty" jsonschema:"new project status"`
	CurrentPhase        *string `json:"current_phase,omitempty" jsonschema:"new current phase"`
	BlueprintArtifactID *string `json:"blueprint_artifact_id,omitempty" jsonschema:"artifact id to declare as the project blueprint (empty string clears; the id must already be registered)"`
	Expect              string  `json:"expect" jsonschema:"version hash from project_get (required: read before you write)"`
}

func registerProjectUpdate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_update",
		Description: "Patch project metadata under the version guard (expect from project_get). Admin operation.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in projectUpdateInput) (*mcpsdk.CallToolResult, app.ProjectView, error) {
		view, err := cfg.service().ProjectUpdate(ctx, app.UpdateProjectRequest{
			Name: in.Name, Description: in.Description, Status: in.Status, CurrentPhase: in.CurrentPhase,
			BlueprintArtifactID: in.BlueprintArtifactID,
			Expect:              in.Expect,
		})
		if err != nil {
			return fail[app.ProjectView](err)
		}
		return nil, view, nil
	})
}

// --- project_state_update ------------------------------------------------

type projectStateUpdateInput struct {
	Summary   *string  `json:"summary,omitempty" jsonschema:"new summary"`
	Risks     []string `json:"risks,omitempty" jsonschema:"replaces the risks list"`
	Blockers  []string `json:"blockers,omitempty" jsonschema:"replaces the blockers list"`
	NextFocus []string `json:"next_focus,omitempty" jsonschema:"replaces the next focus list"`
	Expect    string   `json:"expect" jsonschema:"version hash from the matching read (required: read before you write)"`
}

func registerProjectStateUpdate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "project_state_update",
		Description: "Patch state/current.yaml (summary, risks, blockers, next focus) under the version guard. Admin operation.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in projectStateUpdateInput) (*mcpsdk.CallToolResult, app.StateView, error) {
		view, err := cfg.service().ProjectStateUpdate(ctx, app.UpdateStateRequest{
			Summary: in.Summary, Risks: in.Risks, Blockers: in.Blockers, NextFocus: in.NextFocus,
			Expect: in.Expect,
		})
		if err != nil {
			return fail[app.StateView](err)
		}
		return nil, view, nil
	})
}
