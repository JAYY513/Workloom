// Package domain defines the versioned, text-persisted project records.
package domain

import "time"

const SchemaVersion = 1

type Project struct {
	SchemaVersion       int          `json:"schema_version" yaml:"schema_version"`
	ID                  string       `json:"id" yaml:"id"`
	Name                string       `json:"name" yaml:"name"`
	Description         string       `json:"description" yaml:"description"`
	Status              string       `json:"status" yaml:"status"`
	CurrentPhase        string       `json:"current_phase" yaml:"current_phase"`
	Goals               []string     `json:"goals" yaml:"goals"`
	Scope               Scope        `json:"scope" yaml:"scope"`
	Constraints         []string     `json:"constraints" yaml:"constraints"`
	TechStack           []string     `json:"tech_stack" yaml:"tech_stack"`
	Milestones          []Milestone  `json:"milestones" yaml:"milestones"`
	CurrentState        CurrentState `json:"current_state" yaml:"current_state"`
	BlueprintArtifactID string       `json:"blueprint_artifact_id" yaml:"blueprint_artifact_id"`
	CreatedAt           time.Time    `json:"created_at" yaml:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at" yaml:"updated_at"`
}
type Scope struct {
	In  []string `json:"in" yaml:"in"`
	Out []string `json:"out" yaml:"out"`
}
type Milestone struct {
	ID     string `json:"id" yaml:"id"`
	Name   string `json:"name" yaml:"name"`
	Status string `json:"status" yaml:"status"`
}
type CurrentState struct {
	Summary   string   `json:"summary" yaml:"summary"`
	Risks     []string `json:"risks" yaml:"risks"`
	Blockers  []string `json:"blockers" yaml:"blockers"`
	NextFocus []string `json:"next_focus" yaml:"next_focus"`
}
type CurrentStateFile struct {
	SchemaVersion int `json:"schema_version" yaml:"schema_version"`
	CurrentState  `json:",inline" yaml:",inline"`
}
type MilestonesFile struct {
	SchemaVersion int         `json:"schema_version" yaml:"schema_version"`
	Milestones    []Milestone `json:"milestones" yaml:"milestones"`
}
type Reference struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
}
type WorkflowInstance struct {
	ID            string    `json:"id" yaml:"id"`
	Step          string    `json:"step" yaml:"step"`
	StepEnteredAt time.Time `json:"step_entered_at" yaml:"step_entered_at"`
}
type WorkItem struct {
	SchemaVersion       int               `json:"schema_version" yaml:"schema_version"`
	ID                  string            `json:"id" yaml:"id"`
	ProjectID           string            `json:"project_id" yaml:"project_id"`
	ParentID            *string           `json:"parent_id" yaml:"parent_id"`
	Type                string            `json:"type" yaml:"type"`
	Title               string            `json:"title" yaml:"title"`
	Description         string            `json:"description" yaml:"description"`
	Status              string            `json:"status" yaml:"status"`
	Priority            int               `json:"priority" yaml:"priority"`
	Dependencies        []string          `json:"dependencies" yaml:"dependencies"`
	AcceptanceCriteria  []string          `json:"acceptance_criteria" yaml:"acceptance_criteria"`
	Constraints         []string          `json:"constraints" yaml:"constraints"`
	ContextRefs         []string          `json:"context_refs" yaml:"context_refs"`
	ArtifactRefs        []string          `json:"artifact_refs" yaml:"artifact_refs"`
	CreatedFrom         *Reference        `json:"created_from" yaml:"created_from"`
	Reason              string            `json:"reason" yaml:"reason"`
	ProposedBy          string            `json:"proposed_by" yaml:"proposed_by"`
	ApprovalRequired    bool              `json:"approval_required" yaml:"approval_required"`
	ClarificationNeeded bool              `json:"clarification_needed" yaml:"clarification_needed"`
	Workflow            *WorkflowInstance `json:"workflow" yaml:"workflow"`
	AssignedAgent       *string           `json:"assigned_agent" yaml:"assigned_agent"`
	AssignedHarness     *string           `json:"assigned_harness" yaml:"assigned_harness"`
	CreatedAt           time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at" yaml:"updated_at"`
}
type RunAgent struct {
	ID      string `json:"id" yaml:"id"`
	Harness string `json:"harness" yaml:"harness"`
	Model   string `json:"model" yaml:"model"`
}
type Workspace struct {
	Path     string `json:"path" yaml:"path"`
	Branch   string `json:"branch" yaml:"branch"`
	Worktree string `json:"worktree" yaml:"worktree"`
}
type Claim struct {
	HeadSHA     string     `json:"head_sha" yaml:"head_sha"`
	ClaimedAt   *time.Time `json:"claimed_at" yaml:"claimed_at"`
	LeaseUntil  *time.Time `json:"lease_until" yaml:"lease_until"`
	HeartbeatAt *time.Time `json:"heartbeat_at" yaml:"heartbeat_at"`
}
type Verification struct {
	Advanced          *bool   `json:"advanced" yaml:"advanced"`
	HeadSHAAtComplete string  `json:"head_sha_at_complete" yaml:"head_sha_at_complete"`
	VerifiedBy        *string `json:"verified_by" yaml:"verified_by"`
}
type RunResult struct {
	Summary           *string  `json:"summary" yaml:"summary"`
	Errors            []string `json:"errors" yaml:"errors"`
	Findings          []string `json:"findings" yaml:"findings"`
	Decisions         []string `json:"decisions" yaml:"decisions"`
	FollowUpWorkItems []string `json:"follow_up_workitems" yaml:"follow_up_workitems"`
}

// ContextSnapshot records what the agent actually saw when a run started
// (方案 §11.3): the versions of everything the run's context was built from,
// so later reads can reconstruct the view and trace stale-context errors.
type ContextSnapshot struct {
	ProjectStateVersion int      `json:"project_state_version" yaml:"project_state_version"`
	WorkItemVersion     int      `json:"workitem_version" yaml:"workitem_version"`
	ArtifactVersions    []string `json:"artifact_versions" yaml:"artifact_versions"`
	KnowledgeRevision   string   `json:"knowledge_revision" yaml:"knowledge_revision"`
	WorkspaceHead       string   `json:"workspace_head" yaml:"workspace_head"`
	DecisionIDs         []string `json:"decision_ids" yaml:"decision_ids"`
}
type Run struct {
	SchemaVersion    int              `json:"schema_version" yaml:"schema_version"`
	ID               string           `json:"id" yaml:"id"`
	ProjectID        string           `json:"project_id" yaml:"project_id"`
	WorkItemID       string           `json:"workitem_id" yaml:"workitem_id"`
	WorkflowID       string           `json:"workflow_id" yaml:"workflow_id"`
	Agent            RunAgent         `json:"agent" yaml:"agent"`
	Workspace        Workspace        `json:"workspace" yaml:"workspace"`
	Claim            Claim            `json:"claim" yaml:"claim"`
	Verification     Verification     `json:"verification" yaml:"verification"`
	Status           string           `json:"status" yaml:"status"`
	Attempt          int              `json:"attempt" yaml:"attempt"`
	Phase            string           `json:"phase" yaml:"phase"`
	StartedAt        time.Time        `json:"started_at" yaml:"started_at"`
	FinishedAt       *time.Time       `json:"finished_at" yaml:"finished_at"`
	InputContextRefs []string         `json:"input_context_refs" yaml:"input_context_refs"`
	ChangedFiles     []string         `json:"changed_files" yaml:"changed_files"`
	Commands         []string         `json:"commands" yaml:"commands"`
	Tests            []string         `json:"tests" yaml:"tests"`
	Logs             []string         `json:"logs" yaml:"logs"`
	Result           RunResult        `json:"result" yaml:"result"`
	RetryCount       int              `json:"retry_count" yaml:"retry_count"`
	ContextSnapshot  *ContextSnapshot `json:"context_snapshot" yaml:"context_snapshot"`
}
type Artifact struct {
	SchemaVersion    int       `json:"schema_version" yaml:"schema_version"`
	ID               string    `json:"id" yaml:"id"`
	ProjectID        string    `json:"project_id" yaml:"project_id"`
	Type             string    `json:"type" yaml:"type"`
	Name             string    `json:"name" yaml:"name"`
	Path             string    `json:"path" yaml:"path"`
	Source           string    `json:"source" yaml:"source"`
	CreatedByRunID   string    `json:"created_by_run_id" yaml:"created_by_run_id"`
	Status           string    `json:"status" yaml:"status"`
	Version          int       `json:"version" yaml:"version"`
	PreviousID       *string   `json:"previous_id" yaml:"previous_id"`
	RelatedWorkItems []string  `json:"related_workitems" yaml:"related_workitems"`
	CreatedAt        time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" yaml:"updated_at"`
}
type Decision struct {
	SchemaVersion    int       `json:"schema_version" yaml:"schema_version"`
	ID               string    `json:"id" yaml:"id"`
	ProjectID        string    `json:"project_id" yaml:"project_id"`
	Title            string    `json:"title" yaml:"title"`
	Context          string    `json:"context" yaml:"context"`
	Options          []string  `json:"options" yaml:"options"`
	Decision         string    `json:"decision" yaml:"decision"`
	Reasoning        string    `json:"reasoning" yaml:"reasoning"`
	Consequences     []string  `json:"consequences" yaml:"consequences"`
	Status           string    `json:"status" yaml:"status"`
	CreatedBy        string    `json:"created_by" yaml:"created_by"`
	ApprovedBy       string    `json:"approved_by" yaml:"approved_by"`
	RelatedWorkItems []string  `json:"related_workitems" yaml:"related_workitems"`
	CreatedAt        time.Time `json:"created_at" yaml:"created_at"`
}
type Finding struct {
	SchemaVersion      int      `json:"schema_version" yaml:"schema_version"`
	ID                 string   `json:"id" yaml:"id"`
	ProjectID          string   `json:"project_id" yaml:"project_id"`
	Type               string   `json:"type" yaml:"type"`
	Title              string   `json:"title" yaml:"title"`
	Description        string   `json:"description" yaml:"description"`
	Evidence           []string `json:"evidence" yaml:"evidence"`
	Severity           string   `json:"severity" yaml:"severity"`
	Status             string   `json:"status" yaml:"status"`
	DiscoveredByRunID  string   `json:"discovered_by_run_id" yaml:"discovered_by_run_id"`
	RelatedWorkItems   []string `json:"related_workitems" yaml:"related_workitems"`
	RecommendedActions []string `json:"recommended_actions" yaml:"recommended_actions"`
}
type Approval struct {
	SchemaVersion int        `json:"schema_version" yaml:"schema_version"`
	ID            string     `json:"id" yaml:"id"`
	ProjectID     string     `json:"project_id" yaml:"project_id"`
	Scope         string     `json:"scope" yaml:"scope"`
	WorkItemID    string     `json:"workitem_id" yaml:"workitem_id"`
	RunID         *string    `json:"run_id" yaml:"run_id"`
	RequestedBy   string     `json:"requested_by" yaml:"requested_by"`
	RequestedAt   time.Time  `json:"requested_at" yaml:"requested_at"`
	Status        string     `json:"status" yaml:"status"`
	DecidedBy     *string    `json:"decided_by" yaml:"decided_by"`
	DecidedAt     *time.Time `json:"decided_at" yaml:"decided_at"`
	Comment       *string    `json:"comment" yaml:"comment"`
	ConsumedAt    *time.Time `json:"consumed_at" yaml:"consumed_at"`
	CreatedAt     time.Time  `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" yaml:"updated_at"`
}
type Event struct {
	SchemaVersion int         `json:"schema_version" yaml:"schema_version"`
	ID            string      `json:"id" yaml:"id"`
	ProjectID     string      `json:"project_id" yaml:"project_id"`
	Type          string      `json:"type" yaml:"type"`
	Subject       Reference   `json:"subject" yaml:"subject"`
	Time          time.Time   `json:"time" yaml:"time"`
	Actor         string      `json:"actor" yaml:"actor"`
	Related       []Reference `json:"related" yaml:"related"`
	Content       string      `json:"content" yaml:"content"`
	ReplyTo       *string     `json:"reply_to" yaml:"reply_to"`
}
