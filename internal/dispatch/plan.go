// Package dispatch decides which work items a scheduling tick may start
// (方案 §4.8/§7.4): the ordering, the concurrency gates and the preconditions,
// as a pure function of the state the caller read.
//
// Nothing here writes and nothing here starts anything: the application
// service performs recovery, calls Plan, and starts the attempts it returns.
// Keeping the decision pure is what makes the tick reproducible — the same
// state yields the same plan, and a test can state the expectation exactly.
package dispatch

import (
	"sort"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

// Skip reasons, in the order the planner checks them.
const (
	ReasonNotDispatchable = "not_dispatchable"
	ReasonLeaseActive     = "lease_active"
	ReasonRetryNotDue     = "retry_not_due"
	ReasonBlockedBy       = "blocked_by"
	ReasonMaxReached      = "max_reached"
	ReasonCapGlobal       = "cap_global"
	ReasonCapStatus       = "cap_status"
)

// Caps are the concurrency bounds a tick enforces. Global is the number of
// attempts that may be in flight at once; PerStatus bounds each work item
// status. A zero or negative bound means "not declared"; the caller resolves
// the policy defaults before calling Plan.
type Caps struct {
	Global    int
	PerStatus map[string]int
}

// Decision is what the planner decided about one work item.
type Decision struct {
	WorkitemID string    `json:"workitem_id"`
	Action     string    `json:"action"` // start | skip
	Reason     string    `json:"reason,omitempty"`
	Priority   int       `json:"priority"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// Report is one tick's plan.
type Report struct {
	Start []Decision `json:"start"`
	Skip  []Decision `json:"skip"`
	Caps  Caps       `json:"caps"`
	// InFlight and InFlightByStatus describe what the tick found already
	// running; they are not updated by the plan's own decisions.
	InFlight         int            `json:"in_flight"`
	InFlightByStatus map[string]int `json:"in_flight_by_status"`
}

// Input is everything the plan depends on.
type Input struct {
	Now   time.Time
	Items []*domain.WorkItem
	Caps  Caps
	// Max additionally bounds how many attempts this tick starts (0 = only
	// the caps apply).
	Max int
}

// activeState reports whether a work item is held by a live claim. The tick
// recovers expired leases before planning, so what remains claimed or running
// is genuinely in flight.
func activeState(state string) bool {
	return state == domain.SchedulingClaimed || state == domain.SchedulingRunning
}

// Plan orders the dispatchable work items and applies the gates.
//
// Order (方案 §7.4): priority descending, then oldest created_at, then the
// identifier — a total order, so two ticks over the same state decide the same
// way. Preconditions are checked before the caps: a blocked or not-yet-due item
// is reported as such rather than as an over-cap item, because that is the
// reason an operator can act on.
func Plan(in Input) Report {
	rep := Report{
		Caps:             in.Caps,
		Start:            []Decision{},
		Skip:             []Decision{},
		InFlightByStatus: map[string]int{},
	}
	status := make(map[string]string, len(in.Items))
	for _, wi := range in.Items {
		status[wi.ID] = wi.Status
	}

	// In-flight attempts occupy capacity: they are counted before this tick
	// decides anything.
	globalUse := 0
	for _, wi := range in.Items {
		if !activeState(wi.SchedulingState) {
			continue
		}
		globalUse++
		rep.InFlightByStatus[wi.Status]++
	}
	rep.InFlight = globalUse
	statusUse := make(map[string]int, len(rep.InFlightByStatus))
	for status, count := range rep.InFlightByStatus {
		statusUse[status] = count
	}

	ordered := make([]*domain.WorkItem, len(in.Items))
	copy(ordered, in.Items)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})

	started := 0
	for _, wi := range ordered {
		decision := Decision{
			WorkitemID: wi.ID, Priority: wi.Priority, Status: wi.Status, CreatedAt: wi.CreatedAt,
		}
		switch {
		case !dispatchableStatus(wi.Status):
			decision.Action, decision.Reason = "skip", ReasonNotDispatchable
		case activeState(wi.SchedulingState):
			decision.Action, decision.Reason = "skip", ReasonLeaseActive
		case wi.NextAttemptAt != nil && wi.NextAttemptAt.After(in.Now):
			decision.Action, decision.Reason = "skip", ReasonRetryNotDue
		case !dependenciesSatisfied(wi, status):
			decision.Action, decision.Reason = "skip", ReasonBlockedBy
		case in.Max > 0 && started >= in.Max:
			decision.Action, decision.Reason = "skip", ReasonMaxReached
		case rep.Caps.Global > 0 && globalUse >= rep.Caps.Global:
			decision.Action, decision.Reason = "skip", ReasonCapGlobal
		case rep.Caps.PerStatus[wi.Status] > 0 && statusUse[wi.Status] >= rep.Caps.PerStatus[wi.Status]:
			decision.Action, decision.Reason = "skip", ReasonCapStatus
		default:
			decision.Action = "start"
			started++
			globalUse++
			statusUse[wi.Status]++
			rep.Start = append(rep.Start, decision)
			continue
		}
		rep.Skip = append(rep.Skip, decision)
	}
	return rep
}

// dispatchableStatus is what a tick may start: a ready work item, or one that
// is waiting for its next attempt (its due time is checked separately).
func dispatchableStatus(status string) bool {
	return status == domain.StatusReady || status == domain.StatusRetryQueued
}

// dependenciesSatisfied reports whether every dependency is resolved: done, or
// cancelled — a cancelled dependency will never complete, and waiting for it
// would hold the queue forever. A dependency that is not among the items is
// unresolved: the tick must not guess about state it cannot see.
func dependenciesSatisfied(wi *domain.WorkItem, status map[string]string) bool {
	for _, dep := range wi.Dependencies {
		switch status[dep] {
		case domain.StatusDone, domain.StatusCancelled:
		default:
			return false
		}
	}
	return true
}

// DefaultGlobalCap is the concurrency bound a tick enforces when no policy
// declares one: a policy that says nothing runs one attempt at a time.
const DefaultGlobalCap = 1

// ResolveCaps folds the declared concurrency of the given policies into the
// bounds one tick enforces: the strictest declaration wins, and a policy that
// declares nothing counts as the default — so a silent policy pins the queue to
// one at a time rather than letting a looser neighbour widen it. Each status
// keeps the strictest bound declared for it.
func ResolveCaps(policies []Caps) Caps {
	out := Caps{Global: DefaultGlobalCap, PerStatus: map[string]int{}}
	declared := false
	for _, caps := range policies {
		global := caps.Global
		if global <= 0 {
			global = DefaultGlobalCap
		}
		if !declared || global < out.Global {
			out.Global = global
			declared = true
		}
		for status, limit := range caps.PerStatus {
			if limit <= 0 {
				continue
			}
			if current, ok := out.PerStatus[status]; !ok || limit < current {
				out.PerStatus[status] = limit
			}
		}
	}
	return out
}
