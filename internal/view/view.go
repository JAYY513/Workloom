// Package view assembles the read-only workspace view (方案 §17, 实施计划 M7.1):
// one snapshot of a project's blueprint, progress, runs, records and knowledge
// freshness, built from the .devsys/ state files and git state.
//
// The package never writes. Reads go through storage.Inspect — a shared lock on
// an existing lock file, never created — with storage.InspectUnlocked as the
// fallback for copies where no writer ever ran (a read-only mount of a fresh
// checkout). It never recovers transactions, never repairs torn tails, never
// touches .devsys/.cache/ and never probes a generator (a probe runs a
// process). While pending transactions exist the model reports them and renders
// no business facts (方案 §15.4).
//
// Two builds of the same state are byte-identical: the model carries no
// wall-clock field, and every list has the documented order below.
package view

import (
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/next"
)

// SchemaVersion is the model version this build produces.
const SchemaVersion = 1

// DefaultLimit caps the entries of the list sections (runs, records, pages).
// Options.Limit overrides it; work items and milestones are never capped —
// they are the board itself.
const DefaultLimit = 50

// Trust states (see Trust).
const (
	// TrustOK: reads ran under the shared project lock.
	TrustOK = "ok"
	// TrustAdvisory: no lock file exists (no writer ever ran), so reads were
	// lock-free; the snapshot is advisory (storage.InspectUnlocked).
	TrustAdvisory = "advisory_unlocked"
	// TrustPending: unfinished transactions exist; no business fact is
	// rendered (方案 §15.4).
	TrustPending = "pending_transaction"
)

// Knowledge statuses. fresh/stale/missing mirror app.KnowledgeStatusView; the
// exit codes 0/10/11 belong to `devsys knowledge status` and stay there.
const (
	KnowledgeFresh       = "fresh"
	KnowledgeStale       = "stale"
	KnowledgeMissing     = "missing"
	KnowledgeUnavailable = "unavailable"
)

// Options tunes Build; zero values are valid.
type Options struct {
	// Now supplies the clock for the readiness evaluation (lease expiry,
	// stale review). It is never written into the model.
	Now func() time.Time
	// Limit caps the list sections. Zero selects DefaultLimit.
	Limit int
}

// Model is the assembled view. Field order is the JSON key order; sections
// carry the state files they were read from (方案 §17: every view names its
// source files and its baseline commit — the commit is Model.Baseline).
type Model struct {
	SchemaVersion int       `json:"schema_version"`
	Project       Project   `json:"project"`
	Trust         Trust     `json:"trust"`
	Baseline      Baseline  `json:"baseline"`
	Progress      Progress  `json:"progress"`
	Runs          Runs      `json:"runs"`
	Records       Records   `json:"records"`
	Knowledge     Knowledge `json:"knowledge"`
	// Sources is the sorted union of every section's sources.
	Sources []string `json:"sources"`
	// Problems are located read or decode defects (a corrupt item file, an
	// invalid page, a broken policy). A section with problems is marked
	// Degraded; the view reports them instead of failing.
	Problems []string `json:"problems,omitempty"`
}

// Provenance names the state files a section was assembled from, as
// project-relative slash paths (e.g. ".devsys/project.yaml").
type Provenance struct {
	Sources []string `json:"sources"`
}

// Baseline is the git state at read time. Available is false when git could
// not answer (not a repository, git missing); Dirty is null then, never false.
type Baseline struct {
	Available    bool   `json:"available"`
	Commit       string `json:"commit,omitempty"`
	Branch       string `json:"branch,omitempty"`
	Dirty        *bool  `json:"dirty,omitempty"`
	ChangedFiles int    `json:"changed_files,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Trust says how the state was read and whether it may be believed.
type Trust struct {
	State        string   `json:"state"`
	InspectionOK bool     `json:"inspection_ok"`
	Note         string   `json:"note,omitempty"`
	Pending      []string `json:"pending_transactions"`
}

// Project is the blueprint: identity, goals, scope, constraints and the
// current state of the work (方案 §5.1).
type Project struct {
	Provenance
	ID           string             `json:"id,omitempty"`
	Name         string             `json:"name,omitempty"`
	Description  string             `json:"description,omitempty"`
	Status       string             `json:"status,omitempty"`
	CurrentPhase string             `json:"current_phase,omitempty"`
	Goals        []string           `json:"goals,omitempty"`
	Scope        domain.Scope       `json:"scope"`
	Constraints  []string           `json:"constraints,omitempty"`
	TechStack    []string           `json:"tech_stack,omitempty"`
	Milestones   []domain.Milestone `json:"milestones"`
	Summary      string             `json:"summary,omitempty"`
	Risks        []string           `json:"risks"`
	Blockers     []string           `json:"blockers"`
	NextFocus    []string           `json:"next_focus"`
	CreatedAt    *time.Time         `json:"created_at,omitempty"`
	UpdatedAt    *time.Time         `json:"updated_at,omitempty"`
	// Degraded is true when the section rendered no business facts.
	Degraded bool   `json:"degraded,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Progress is the board: every work item with the scheduling facts, plus the
// §7.4 readiness verdict and its next action.
type Progress struct {
	Provenance
	Counts map[string]int `json:"counts"`
	Items  []Item         `json:"items"`
	// Readiness is internal/next's evaluation, the same verdict `devsys next`
	// reports; empty risks/fixes mean none were observed.
	Readiness next.Report `json:"readiness"`
	Degraded  bool        `json:"degraded,omitempty"`
	Reason    string      `json:"reason,omitempty"`
}

// Item is one work item, trimmed to what a view renders (方案 §17 任务视图).
type Item struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Type            string     `json:"type,omitempty"`
	Status          string     `json:"status"`
	Priority        int        `json:"priority"`
	ParentID        string     `json:"parent_id,omitempty"`
	Dependencies    []string   `json:"dependencies,omitempty"`
	Workflow        string     `json:"workflow,omitempty"`
	WorkflowStep    string     `json:"workflow_step,omitempty"`
	WorkflowPaused  bool       `json:"workflow_paused,omitempty"`
	AssignedAgent   string     `json:"assigned_agent,omitempty"`
	AssignedHarness string     `json:"assigned_harness,omitempty"`
	SchedulingState string     `json:"scheduling_state,omitempty"`
	LeaseOwner      string     `json:"lease_owner,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty"`
	ActiveRunID     string     `json:"active_run_id,omitempty"`
	PreviousStatus  string     `json:"previous_status,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Runs is the execution timeline, newest first (StartedAt descending, then ID).
type Runs struct {
	Provenance
	Entries   []RunEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
}

// RunEntry is one run, trimmed to the §17 Run view: agent, harness, model,
// workspace, status, attempt and the completion verdict.
type RunEntry struct {
	ID         string     `json:"id"`
	WorkitemID string     `json:"workitem_id,omitempty"`
	WorkflowID string     `json:"workflow_id,omitempty"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase,omitempty"`
	Harness    string     `json:"harness,omitempty"`
	Model      string     `json:"model,omitempty"`
	AgentID    string     `json:"agent_id,omitempty"`
	Workspace  string     `json:"workspace,omitempty"`
	Branch     string     `json:"branch,omitempty"`
	Attempt    int        `json:"attempt"`
	RetryCount int        `json:"retry_count,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Advanced   *bool      `json:"advanced,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Errors     []string   `json:"errors,omitempty"`
	// StreamTorn is true when the run's event stream has an unreadable tail
	// (a crash left it half-written); the view never repairs it.
	StreamTorn bool `json:"stream_torn,omitempty"`
}

// Records are the decision, finding and artifact logs (方案 §17 记录视图).
type Records struct {
	Provenance
	Decisions []RecordRef `json:"decisions"`
	Findings  []RecordRef `json:"findings"`
	Artifacts []RecordRef `json:"artifacts"`
	Truncated bool        `json:"truncated,omitempty"`
}

// RecordRef is one decision, finding or artifact, trimmed to its identity and
// the work items it names.
type RecordRef struct {
	ID               string     `json:"id"`
	Title            string     `json:"title,omitempty"`
	Type             string     `json:"type,omitempty"`
	Status           string     `json:"status,omitempty"`
	Severity         string     `json:"severity,omitempty"`
	Version          int        `json:"version,omitempty"`
	RelatedWorkItems []string   `json:"related_workitems,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
}

// Knowledge is the page layer's index and freshness (M5.3, 方案 §12.5/§17).
type Knowledge struct {
	Provenance
	// Status is fresh, stale, missing (no page layer: a supported
	// degradation) or unavailable (the freshness question could not be
	// answered — e.g. no git). "unavailable" never claims fresh.
	Status       string      `json:"status"`
	Reason       string      `json:"reason,omitempty"`
	Baseline     string      `json:"baseline,omitempty"`
	Head         string      `json:"head,omitempty"`
	Branch       string      `json:"branch,omitempty"`
	IndexReady   bool        `json:"index_ready"`
	IndexFiles   int         `json:"index_files,omitempty"`
	Generator    string      `json:"generator,omitempty"`
	Adapter      string      `json:"adapter,omitempty"`
	Pages        []PageEntry `json:"pages"`
	Affected     []string    `json:"affected"`
	Unverifiable []string    `json:"unverifiable,omitempty"`
	ChangedFiles int         `json:"changed_files"`
	Truncated    bool        `json:"truncated,omitempty"`
	Degraded     bool        `json:"degraded,omitempty"`
}

// PageEntry is one knowledge page with its freshness verdict and the work
// items it is relevant to (trigger match, the M5.6 rule).
type PageEntry struct {
	Path             string   `json:"path"`
	Type             string   `json:"type,omitempty"`
	Status           string   `json:"status,omitempty"`
	SourceCommit     string   `json:"source_commit,omitempty"`
	Stale            bool     `json:"stale"`
	Reason           string   `json:"reason,omitempty"`
	RelatedWorkItems []string `json:"related_workitems,omitempty"`
}
