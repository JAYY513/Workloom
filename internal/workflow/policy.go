// Package workflow parses and validates project workflow policy files
// (.devsys/workflows/<id>.md: YAML front matter plus a prompt body, 方案 §5.3,
// 实施计划 M3.1).
//
// Parsing is strict and located: structural, type, required and
// cross-reference defects are collected with file, line, field and reason —
// the rendering internal/config established for managed files — and a file
// with any error-severity issue yields no policy. Unknown keys are recorded as
// warnings instead of errors so a policy written for a newer build stays
// loadable (实施计划 M3.1: 记录但不崩).
package workflow

import (
	"fmt"
	"strings"
)

// Severity classifies one parsing issue.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one located defect or recorded observation in a policy file.
type Issue struct {
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Field    string   `json:"field,omitempty"`
	Reason   string   `json:"reason"`
	Severity Severity `json:"severity"`
}

// String renders the location as `file[:line][: field]: reason`, matching the
// config.Problem rendering.
func (i Issue) String() string {
	var b strings.Builder
	b.WriteString(i.File)
	if i.Line > 0 {
		fmt.Fprintf(&b, ":%d", i.Line)
	}
	if i.Field != "" {
		b.WriteString(": ")
		b.WriteString(i.Field)
	}
	b.WriteString(": ")
	b.WriteString(i.Reason)
	return b.String()
}

// Input lists the context a policy expects when a work item starts (方案 §5.3).
type Input struct {
	Required []string
	Optional []string
}

// Step is one declared workflow step.
type Step struct {
	ID       string
	Type     string
	Required bool
	// Status is the work item status required on entering the step; empty when
	// the step does not declare one (§5.3 附加规则).
	Status string
}

// Transition is one declared edge between steps.
type Transition struct {
	From string
	To   string // a step id or "done"
	When string // declarative condition; empty means unconditional
	// Cond is the parsed, load-validated condition; nil when When is empty.
	Cond *Condition
}

// Gate is the stage gate for one work item status (方案 §4.7).
type Gate struct {
	RequireArtifacts    []string
	RequireMinArtifacts int
	RequireComment      bool
	RequireApproval     bool
}

// Gates is the gates section: statuses exempt from gate checks plus per-status
// gates.
type Gates struct {
	ExemptStages []string
	Stages       map[string]Gate
}

// Hook is one workspace lifecycle hook command (方案 §4.8).
type Hook struct {
	Command        string
	TimeoutSeconds int
}

// Concurrency caps concurrent execution (方案 §5.3).
type Concurrency struct {
	Global    int
	PerStatus map[string]int
}

// Limits carries attempt and stagnation bounds (方案 §4.8/§5.3).
type Limits struct {
	MaxAttempts           int
	RunTimeoutSeconds     int
	StallThresholdSeconds int
	BackoffMaxSeconds     int
}

// QualityGate is the deterministic claim-time quality threshold (方案 §4.7).
type QualityGate struct {
	MinScore int
}

// Policy is one parsed and validated workflow policy.
type Policy struct {
	// File is the slash-separated path below .devsys/ (e.g.
	// "workflows/quick-fix.md").
	File string

	ID      string
	Name    string
	Version int

	Input           Input
	Steps           []Step
	Transitions     []Transition
	ApprovalPoints  []string
	CompletionRules []string
	Gates           Gates
	Hooks           map[string]Hook
	Concurrency     Concurrency
	Limits          Limits
	QualityGate     QualityGate
	// OnReject is "block" or "regress:<status>"; it defaults to "block" (§4.9).
	OnReject string

	// Body is the prompt template below the front matter, verbatim.
	Body string
}
