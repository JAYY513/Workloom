// Package domain — work item status state machine (§6).
//
// The state machine is conservative: transitions that lower completion
// ("completed" → earlier) are reserved for the repair path; the normal path
// only walks forward or steps sideways (blocked ↔ resume). Terminal states
// reject any further transition except a repair.
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Work item status values (§6.1). All nine are valid; Create allows a
// restricted subset (UnclaimedInitial), Transition enforces the graph below.
const (
	StatusDraft        = "draft"
	StatusBacklog      = "backlog"
	StatusReady        = "ready"
	StatusInProgress   = "in_progress"
	StatusBlocked      = "blocked"
	StatusReview       = "review"
	StatusVerification = "verification"
	StatusDone         = "done"
	StatusCancelled    = "cancelled"
)

// AllStatuses lists every defined status for validation.
func AllStatuses() []string {
	return []string{
		StatusDraft, StatusBacklog, StatusReady, StatusInProgress,
		StatusBlocked, StatusReview, StatusVerification,
		StatusDone, StatusCancelled,
	}
}

// Scheduling states (§6.4): separate from the work item status. The lease
// files under .devsys/scheduling/<id>.mirror of "claimed/running" while
// unclaimed/released map to absence.
const (
	SchedulingUnclaimed   = "unclaimed"
	SchedulingClaimed     = "claimed"
	SchedulingRunning     = "running"
	SchedulingRetryQueued = "retry_queued"
	SchedulingReleased    = "released"
)

// allowedTransitions is the conservative graph. Keys are from-statuses; values
// list every legal target. The key property: no terminal state has any
// outgoing edges except a repair — see IsTransitionLegal and the AllowRepair
// flag in workitem.TransitionRequest.
//
// blocked ↔ previous completion: from blocked, the resume edge may target
// the status the work item held when it became blocked. The caller encodes
// that intent via TransitionRequest.PreviousStatus — see IsResumeLegal.
//
// Forward skips (§6.3 状态变更规则) consistent with §6: ready → review and
// ready → verification are allowed for tasks that don't need a normal
// implementation phase; in_progress → verification skips review when review
// is configured away; verification → done is the standard terminal path.
var allowedTransitions = map[string]map[string]bool{
	StatusDraft: {
		StatusBacklog:   true,
		StatusReady:     true,
		StatusDone:      true, // discarded drafts close cleanly
		StatusCancelled: true,
	},
	StatusBacklog: {
		StatusDraft:     true, // demote: needs more drafting
		StatusReady:     true,
		StatusCancelled: true,
	},
	StatusReady: {
		StatusDraft:        true, // re-scopping pulls it back
		StatusBacklog:      true, // deprioritised
		StatusInProgress:   true, // normal claim → start
		StatusBlocked:      true, // explicit block before claim
		StatusReview:       true, // forward skip: tasks without implementation
		StatusVerification: true,
		StatusCancelled:    true,
	},
	StatusInProgress: {
		StatusReady:        true, // explicit handback
		StatusBlocked:      true,
		StatusReview:       true,
		StatusVerification: true, // forward skip: review folded in
		StatusCancelled:    true,
	},
	StatusBlocked: {
		// Resume edges are validated separately because the legal target
		// depends on the status the work item held before blocking.
		StatusReady:     true, // unblocked → ready (caller responsibility)
		StatusCancelled: true,
	},
	StatusReview: {
		StatusInProgress:   true, // review requested changes
		StatusVerification: true,
		StatusReady:        true, // handback for more work
		StatusBlocked:      true,
		StatusCancelled:    true,
	},
	StatusVerification: {
		StatusInProgress: true, // failed verification needs more work
		StatusReview:     true, // re-review after fix
		StatusDone:       true,
		StatusBlocked:    true,
		StatusCancelled:  true,
	},
	StatusDone:      {}, // terminal — only repair can move it
	StatusCancelled: {}, // terminal — only repair can move it
}

// IsTerminal reports whether status is a terminal state (no normal outgoing
// edges). Used by Create to forbid initial states that don't exist.
func IsTerminal(status string) bool {
	t, ok := allowedTransitions[status]
	return ok && len(t) == 0
}

// UnclaimedInitial lists statuses a Create call may use as the initial status.
// These all map to the unclaimed scheduling state (§6.4 table).
var UnclaimedInitial = map[string]bool{
	StatusDraft:   true,
	StatusBacklog: true,
	StatusReady:   true,
	StatusBlocked: true,
}

// IsTransitionLegal reports whether from → to is allowed by the conservative
// graph. blocked is special-cased via IsResumeLegal.
func IsTransitionLegal(from, to string) bool {
	if from == "" {
		return UnclaimedInitial[to]
	}
	edges, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return edges[to]
}

// IsResumeLegal checks the blocked → previous-completion edge: from blocked,
// resuming into the status the work item held when it was blocked is legal
// regardless of the static graph. Other blocked targets must come from
// IsTransitionLegal.
func IsResumeLegal(from, to, previous string) bool {
	if from != StatusBlocked {
		return false
	}
	if IsTransitionLegal(StatusBlocked, to) {
		return true
	}
	return previous != "" && to == previous
}

// AllowedFrom lists every legal next status from current, sorted. Used to
// build the "allowed: [a, b, c]" hint on transition errors.
func AllowedFrom(current string) []string {
	if current == "" {
		out := make([]string, 0, len(UnclaimedInitial))
		for k := range UnclaimedInitial {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	out := make([]string, 0, len(allowedTransitions[current]))
	for k := range allowedTransitions[current] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TransitionError describes an illegal transition with the allowed list, so
// the caller (CLI/MCP) can render "draft → done is not allowed; next: ..."
// without re-deriving the graph.
type TransitionError struct {
	From    string
	To      string
	Allowed []string
	Reason  string // e.g. "from is terminal", "blocked resume target unknown"
}

func (e *TransitionError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("illegal transition %s → %s: %s (allowed: %s)",
			e.From, e.To, e.Reason, strings.Join(e.Allowed, ", "))
	}
	return fmt.Sprintf("illegal transition %s → %s (allowed: %s)",
		e.From, e.To, strings.Join(e.Allowed, ", "))
}
