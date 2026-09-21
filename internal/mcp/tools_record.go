package mcp

import (
	"context"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/domain"
)

// --- decision_list / get / create / approve ------------------------------

type decisionListInput struct{}

type decisionListResult struct {
	Decisions []*domain.Decision `json:"decisions"`
}

func registerDecisionList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "decision_list",
		Description: "List every recorded decision (identity and body). Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ decisionListInput) (*mcpsdk.CallToolResult, decisionListResult, error) {
		items, err := cfg.service().DecisionList(ctx)
		if err != nil {
			return fail[decisionListResult](err)
		}
		return nil, decisionListResult{Decisions: items}, nil
	})
}

type decisionGetInput struct {
	ID string `json:"id" jsonschema:"decision id, e.g. decision-1"`
}

func registerDecisionGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "decision_get",
		Description: "Read one decision with its version hash. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in decisionGetInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().DecisionGet(ctx, in.ID)
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type decisionCreateInput struct {
	Title            string   `json:"title" jsonschema:"decision title"`
	Decision         string   `json:"decision" jsonschema:"what was decided"`
	Context          string   `json:"context,omitempty" jsonschema:"why the decision was needed"`
	Reasoning        string   `json:"reasoning,omitempty" jsonschema:"the reasoning behind it"`
	Options          []string `json:"options,omitempty" jsonschema:"options considered"`
	Consequences     []string `json:"consequences,omitempty" jsonschema:"expected consequences"`
	RelatedWorkItems []string `json:"related_workitems,omitempty" jsonschema:"work items this decision touches"`
	CreatedBy        string   `json:"created_by" jsonschema:"author identity"`
}

func registerDecisionCreate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "decision_create",
		Description: "Record a decision (one file per record).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in decisionCreateInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().DecisionCreate(ctx, app.CreateDecisionRequest{
			Title: in.Title, Context: in.Context, Decision: in.Decision, Reasoning: in.Reasoning,
			Options: in.Options, Consequences: in.Consequences, RelatedWorkItems: in.RelatedWorkItems,
			CreatedBy: in.CreatedBy,
		})
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type decisionApproveInput struct {
	ID     string `json:"id" jsonschema:"decision id"`
	By     string `json:"by" jsonschema:"decider identity"`
	Expect string `json:"expect" jsonschema:"version hash from decision_get (required: read before you write)"`
}

func registerDecisionApprove(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "decision_approve",
		Description: "Mark a decision approved, under the version guard.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in decisionApproveInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().DecisionApprove(ctx, in.ID, in.By, in.Expect)
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

// --- finding_list / get / create / resolve -------------------------------

type findingListInput struct{}

type findingListResult struct {
	Findings []*domain.Finding `json:"findings"`
}

func registerFindingList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "finding_list",
		Description: "List every recorded finding. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ findingListInput) (*mcpsdk.CallToolResult, findingListResult, error) {
		items, err := cfg.service().FindingList(ctx)
		if err != nil {
			return fail[findingListResult](err)
		}
		return nil, findingListResult{Findings: items}, nil
	})
}

type findingGetInput struct {
	ID string `json:"id" jsonschema:"finding id, e.g. finding-1"`
}

func registerFindingGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "finding_get",
		Description: "Read one finding with its version hash. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in findingGetInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().FindingGet(ctx, in.ID)
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type findingCreateInput struct {
	Type               string   `json:"type,omitempty" jsonschema:"finding type (default issue)"`
	Title              string   `json:"title" jsonschema:"finding title"`
	Description        string   `json:"description" jsonschema:"what was found"`
	Evidence           []string `json:"evidence,omitempty" jsonschema:"paths or references backing the finding"`
	Severity           string   `json:"severity,omitempty" jsonschema:"severity label"`
	RelatedWorkItems   []string `json:"related_workitems,omitempty" jsonschema:"work items this finding touches"`
	RecommendedActions []string `json:"recommended_actions,omitempty" jsonschema:"what to do about it"`
	DiscoveredByRunID  string   `json:"discovered_by_run_id,omitempty" jsonschema:"run that discovered it"`
}

func registerFindingCreate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "finding_create",
		Description: "Record a finding (one file per record).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in findingCreateInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().FindingCreate(ctx, app.CreateFindingRequest{
			Type: in.Type, Title: in.Title, Description: in.Description, Evidence: in.Evidence,
			Severity: in.Severity, RelatedWorkItems: in.RelatedWorkItems,
			RecommendedActions: in.RecommendedActions, DiscoveredByRunID: in.DiscoveredByRunID,
		})
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type findingResolveInput struct {
	ID         string `json:"id" jsonschema:"finding id"`
	Resolution string `json:"resolution,omitempty" jsonschema:"how it was resolved"`
	Expect     string `json:"expect" jsonschema:"version hash from finding_get (required: read before you write)"`
}

func registerFindingResolve(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "finding_resolve",
		Description: "Mark a finding resolved, under the version guard.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in findingResolveInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().FindingResolve(ctx, in.ID, in.Resolution, in.Expect)
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

// --- event_list / event_record -------------------------------------------

type eventListInput struct {
	SubjectType string `json:"subject_type,omitempty" jsonschema:"subject type filter (workitem, run, project)"`
	SubjectID   string `json:"subject_id,omitempty" jsonschema:"subject id filter"`
	Type        string `json:"type,omitempty" jsonschema:"event type filter (e.g. comment)"`
	Since       string `json:"since,omitempty" jsonschema:"RFC3339 lower bound (inclusive)"`
	Until       string `json:"until,omitempty" jsonschema:"RFC3339 upper bound (inclusive)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"keep only the newest N events"`
}

type eventListResult struct {
	Events []*domain.Event `json:"events"`
}

func registerEventList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "event_list",
		Description: "Read the event stream (filterable by subject, type and time window). Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in eventListInput) (*mcpsdk.CallToolResult, eventListResult, error) {
		var since, until time.Time
		if in.Since != "" {
			parsed, err := time.Parse(time.RFC3339, in.Since)
			if err != nil {
				return usageFail[eventListResult]("since must be RFC3339 (got %q)", in.Since)
			}
			since = parsed
		}
		if in.Until != "" {
			parsed, err := time.Parse(time.RFC3339, in.Until)
			if err != nil {
				return usageFail[eventListResult]("until must be RFC3339 (got %q)", in.Until)
			}
			until = parsed
		}
		items, err := cfg.service().EventList(ctx, app.EventListRequest{
			SubjectType: in.SubjectType, SubjectID: in.SubjectID, Type: in.Type,
			Since: since, Until: until, Limit: in.Limit,
		})
		if err != nil {
			return fail[eventListResult](err)
		}
		return nil, eventListResult{Events: items}, nil
	})
}

type eventRecordInput struct {
	Type        string `json:"type" jsonschema:"event type (e.g. note, decision, finding)"`
	SubjectID   string `json:"subject_id" jsonschema:"subject id"`
	SubjectType string `json:"subject_type,omitempty" jsonschema:"subject type (default workitem)"`
	Actor       string `json:"actor" jsonschema:"author identity"`
	Content     string `json:"content,omitempty" jsonschema:"event body"`
	ReplyTo     string `json:"reply_to,omitempty" jsonschema:"event id this event replies to (comment events only)"`
}

type eventRecordResult struct {
	Event *domain.Event `json:"event"`
}

func registerEventRecord(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "event_record",
		Description: "Append one event (append-only; no version guard).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in eventRecordInput) (*mcpsdk.CallToolResult, eventRecordResult, error) {
		subjectType := in.SubjectType
		if subjectType == "" {
			subjectType = "workitem"
		}
		ev, err := cfg.service().EventRecord(ctx, app.RecordEventRequest{
			Type: in.Type, Subject: domain.Reference{Type: subjectType, ID: in.SubjectID},
			Actor: in.Actor, Content: in.Content, ReplyTo: in.ReplyTo,
		})
		if err != nil {
			return fail[eventRecordResult](err)
		}
		return nil, eventRecordResult{Event: ev}, nil
	})
}

// --- artifact_list / get / register / update / history -------------------

type artifactListInput struct{}

type artifactListResult struct {
	Artifacts []*domain.Artifact `json:"artifacts"`
}

func registerArtifactList(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "artifact_list",
		Description: "List every artifact record (latest versions included). Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ artifactListInput) (*mcpsdk.CallToolResult, artifactListResult, error) {
		items, err := cfg.service().ArtifactList(ctx)
		if err != nil {
			return fail[artifactListResult](err)
		}
		return nil, artifactListResult{Artifacts: items}, nil
	})
}

type artifactGetInput struct {
	ID string `json:"id" jsonschema:"artifact id, e.g. artifact-1"`
}

func registerArtifactGet(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "artifact_get",
		Description: "Read one artifact with its version hash. Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in artifactGetInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().ArtifactGet(ctx, in.ID)
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type artifactRegisterInput struct {
	Type             string   `json:"type,omitempty" jsonschema:"artifact type (default document)"`
	Name             string   `json:"name" jsonschema:"artifact name (gates match on this)"`
	Path             string   `json:"path,omitempty" jsonschema:"repository path"`
	Source           string   `json:"source,omitempty" jsonschema:"where it came from"`
	CreatedByRunID   string   `json:"created_by_run_id,omitempty" jsonschema:"run that produced it"`
	Status           string   `json:"status,omitempty" jsonschema:"status (default draft)"`
	RelatedWorkItems []string `json:"related_workitems,omitempty" jsonschema:"work items this artifact belongs to"`
	Actor            string   `json:"actor" jsonschema:"operator identity (required: audit trail)"`
	Reason           string   `json:"reason" jsonschema:"why the registration happens (required: audit trail)"`
}

func registerArtifactRegister(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "artifact_register",
		Description: "Register a new artifact record (version 1).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in artifactRegisterInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().ArtifactRegister(ctx, app.RegisterArtifactRequest{
			Type: in.Type, Name: in.Name, Path: in.Path, Source: in.Source,
			CreatedByRunID: in.CreatedByRunID, Status: in.Status, RelatedWorkItems: in.RelatedWorkItems,
			Actor: in.Actor, Reason: in.Reason,
		})
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type artifactUpdateInput struct {
	ID               string   `json:"id" jsonschema:"artifact id to supersede"`
	Expect           string   `json:"expect" jsonschema:"version hash from artifact_get (required: read before you write)"`
	Status           string   `json:"status,omitempty" jsonschema:"new status"`
	Path             string   `json:"path,omitempty" jsonschema:"new path"`
	Source           string   `json:"source,omitempty" jsonschema:"new source"`
	RelatedWorkItems []string `json:"related_workitems,omitempty" jsonschema:"replaces the related work items"`
}

func registerArtifactUpdate(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "artifact_update",
		Description: "Append a new artifact version linked to the previous one (records are immutable).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in artifactUpdateInput) (*mcpsdk.CallToolResult, app.RecordView, error) {
		view, err := cfg.service().ArtifactUpdate(ctx, app.UpdateArtifactRequest{
			ID: in.ID, Expect: in.Expect, Status: in.Status, Path: in.Path, Source: in.Source,
			RelatedWorkItems: in.RelatedWorkItems,
		})
		if err != nil {
			return fail[app.RecordView](err)
		}
		return nil, view, nil
	})
}

type artifactHistoryInput struct {
	ID string `json:"id" jsonschema:"artifact id (any version)"`
}

type artifactHistoryResult struct {
	Artifacts []*domain.Artifact `json:"artifacts"`
}

func registerArtifactHistory(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "artifact_history",
		Description: "Walk an artifact's version chain (newest first). Read-only.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in artifactHistoryInput) (*mcpsdk.CallToolResult, artifactHistoryResult, error) {
		items, err := cfg.service().ArtifactHistory(ctx, in.ID)
		if err != nil {
			return fail[artifactHistoryResult](err)
		}
		return nil, artifactHistoryResult{Artifacts: items}, nil
	})
}
