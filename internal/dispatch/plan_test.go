package dispatch

import (
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

func item(id string, status string, priority int, created time.Time) *domain.WorkItem {
	return &domain.WorkItem{
		ID: id, Status: status, Priority: priority, CreatedAt: created,
		SchedulingState: domain.SchedulingUnclaimed,
	}
}

func ids(decisions []Decision) []string {
	out := make([]string, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, d.WorkitemID)
	}
	return out
}

func reasonFor(rep Report, id string) string {
	for _, d := range rep.Skip {
		if d.WorkitemID == id {
			return d.Reason
		}
	}
	return ""
}

// The order is the spec's: priority descending, then the oldest, then the
// identifier — a total order, so two ticks over the same state agree.
func TestPlanOrdersByPriorityThenAgeThenID(t *testing.T) {
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	rep := Plan(Input{
		Now: base,
		Items: []*domain.WorkItem{
			item("WLM-3", domain.StatusReady, 5, base.Add(2*time.Hour)),
			item("WLM-2", domain.StatusReady, 9, base.Add(time.Hour)),
			item("WLM-1", domain.StatusReady, 5, base),
			item("WLM-0", domain.StatusReady, 5, base),
		},
		Caps: Caps{Global: 10},
	})
	want := []string{"WLM-2", "WLM-0", "WLM-1", "WLM-3"}
	if got := ids(rep.Start); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("start order = %v, want %v", got, want)
	}
}

// The global bound is enforced against what is already in flight, so a tick
// never exceeds it even when it finds several candidates.
func TestPlanEnforcesGlobalCap(t *testing.T) {
	now := time.Now().UTC()
	running := item("WLM-9", domain.StatusInProgress, 0, now)
	running.SchedulingState = domain.SchedulingRunning
	rep := Plan(Input{
		Now: now,
		Items: []*domain.WorkItem{
			running,
			item("WLM-1", domain.StatusReady, 5, now),
			item("WLM-2", domain.StatusReady, 4, now),
		},
		Caps: Caps{Global: 2},
	})
	if rep.InFlight != 1 {
		t.Fatalf("in flight = %d, want 1", rep.InFlight)
	}
	if got := ids(rep.Start); len(got) != 1 || got[0] != "WLM-1" {
		t.Fatalf("started = %v, want just the highest priority candidate", got)
	}
	if got := reasonFor(rep, "WLM-2"); got != ReasonCapGlobal {
		t.Fatalf("WLM-2 skip reason = %q, want %q", got, ReasonCapGlobal)
	}
}

// The per-status bound applies on top of the global one.
func TestPlanEnforcesPerStatusCap(t *testing.T) {
	now := time.Now().UTC()
	rep := Plan(Input{
		Now: now,
		Items: []*domain.WorkItem{
			item("WLM-1", domain.StatusReady, 5, now),
			item("WLM-2", domain.StatusReady, 4, now),
		},
		Caps: Caps{Global: 5, PerStatus: map[string]int{domain.StatusReady: 1}},
	})
	if got := ids(rep.Start); len(got) != 1 {
		t.Fatalf("started = %v, want one", got)
	}
	if got := reasonFor(rep, "WLM-2"); got != ReasonCapStatus {
		t.Fatalf("skip reason = %q, want %q", got, ReasonCapStatus)
	}
}

// An item whose dependencies are unresolved is skipped, and the reason names
// the precondition rather than a cap: that is what an operator can act on.
func TestPlanBlocksOnUnresolvedDependencies(t *testing.T) {
	now := time.Now().UTC()
	blocked := item("WLM-2", domain.StatusReady, 9, now)
	blocked.Dependencies = []string{"WLM-1"}
	done := item("WLM-1", domain.StatusInProgress, 1, now)
	rep := Plan(Input{Now: now, Items: []*domain.WorkItem{blocked, done}, Caps: Caps{Global: 5}})
	if got := ids(rep.Start); len(got) != 0 {
		t.Fatalf("started = %v, want nothing while the dependency is unresolved", got)
	}
	if got := reasonFor(rep, "WLM-2"); got != ReasonBlockedBy {
		t.Fatalf("skip reason = %q, want %q", got, ReasonBlockedBy)
	}

	// The same item with a resolved dependency is a candidate.
	done.Status = domain.StatusDone
	rep = Plan(Input{Now: now, Items: []*domain.WorkItem{blocked, done}, Caps: Caps{Global: 5}})
	if got := ids(rep.Start); len(got) != 1 || got[0] != "WLM-2" {
		t.Fatalf("started = %v, want the unblocked item", got)
	}
	// A cancelled dependency is resolved too: it will never complete.
	done.Status = domain.StatusCancelled
	rep = Plan(Input{Now: now, Items: []*domain.WorkItem{blocked, done}, Caps: Caps{Global: 5}})
	if got := ids(rep.Start); len(got) != 1 {
		t.Fatalf("started = %v, want the item unblocked by the cancellation", got)
	}
	// A dependency the tick cannot see is unresolved.
	rep = Plan(Input{Now: now, Items: []*domain.WorkItem{blocked}, Caps: Caps{Global: 5}})
	if got := reasonFor(rep, "WLM-2"); got != ReasonBlockedBy {
		t.Fatalf("skip reason = %q, want %q", got, ReasonBlockedBy)
	}
}

// Held items, items that are not ready, and items whose retry is not due yet
// are all skipped with their own reason.
func TestPlanSkipReasons(t *testing.T) {
	now := time.Now().UTC()
	held := item("WLM-1", domain.StatusReady, 5, now)
	held.SchedulingState = domain.SchedulingClaimed
	backlog := item("WLM-2", domain.StatusBacklog, 5, now)
	later := item("WLM-3", domain.StatusReady, 5, now)
	due := now.Add(time.Hour)
	later.NextAttemptAt = &due
	dueNow := item("WLM-4", domain.StatusReady, 5, now)
	past := now.Add(-time.Minute)
	dueNow.NextAttemptAt = &past

	rep := Plan(Input{Now: now, Items: []*domain.WorkItem{held, backlog, later, dueNow}, Caps: Caps{Global: 5}})
	if got := reasonFor(rep, "WLM-1"); got != ReasonLeaseActive {
		t.Fatalf("held item reason = %q, want %q", got, ReasonLeaseActive)
	}
	if got := reasonFor(rep, "WLM-2"); got != ReasonNotDispatchable {
		t.Fatalf("backlog item reason = %q, want %q", got, ReasonNotDispatchable)
	}
	if got := reasonFor(rep, "WLM-3"); got != ReasonRetryNotDue {
		t.Fatalf("retry item reason = %q, want %q", got, ReasonRetryNotDue)
	}
	if got := ids(rep.Start); len(got) != 1 || got[0] != "WLM-4" {
		t.Fatalf("started = %v, want the item whose retry is due", got)
	}
}

// Max bounds one tick without touching the caps.
func TestPlanMaxBoundsOneTick(t *testing.T) {
	now := time.Now().UTC()
	rep := Plan(Input{
		Now: now,
		Items: []*domain.WorkItem{
			item("WLM-1", domain.StatusReady, 5, now),
			item("WLM-2", domain.StatusReady, 4, now),
			item("WLM-3", domain.StatusReady, 3, now),
		},
		Caps: Caps{Global: 10}, Max: 2,
	})
	if got := ids(rep.Start); len(got) != 2 || got[0] != "WLM-1" || got[1] != "WLM-2" {
		t.Fatalf("started = %v, want the first two", got)
	}
	if got := reasonFor(rep, "WLM-3"); got != ReasonMaxReached {
		t.Fatalf("skip reason = %q, want %q", got, ReasonMaxReached)
	}
}

func TestPlanEmptyInput(t *testing.T) {
	rep := Plan(Input{Now: time.Now().UTC(), Caps: Caps{Global: 1}})
	if len(rep.Start) != 0 || len(rep.Skip) != 0 || rep.InFlight != 0 {
		t.Fatalf("plan = %+v, want an empty report", rep)
	}
}

// The resolved bounds take the strictest declaration: a global bound stays
// global, and a policy that declares nothing falls back to one at a time.
func TestResolveCapsTakesTheStrictest(t *testing.T) {
	caps := ResolveCaps([]Caps{
		{Global: 4, PerStatus: map[string]int{domain.StatusReady: 3}},
		{Global: 2, PerStatus: map[string]int{domain.StatusReady: 5, domain.StatusInProgress: 1}},
	})
	if caps.Global != 2 {
		t.Fatalf("global = %d, want the strictest (2)", caps.Global)
	}
	if caps.PerStatus[domain.StatusReady] != 3 || caps.PerStatus[domain.StatusInProgress] != 1 {
		t.Fatalf("per-status = %v, want the strictest per status", caps.PerStatus)
	}
	if got := ResolveCaps(nil); got.Global != DefaultGlobalCap {
		t.Fatalf("default global = %d, want %d", got.Global, DefaultGlobalCap)
	}
}

// A policy that declares nothing counts as the default, so it pins the queue
// to one at a time instead of letting a looser neighbour widen the bound.
func TestResolveCapsTreatsSilenceAsTheDefault(t *testing.T) {
	caps := ResolveCaps([]Caps{
		{Global: 10, PerStatus: map[string]int{domain.StatusReady: 5}},
		{},
	})
	if caps.Global != DefaultGlobalCap {
		t.Fatalf("global = %d, want %d: a silent policy is the strictest", caps.Global, DefaultGlobalCap)
	}
	if caps.PerStatus[domain.StatusReady] != 5 {
		t.Fatalf("per-status = %v, want the declared bound", caps.PerStatus)
	}
}

// The report's in-flight figures describe what the tick found, not what it
// decided: a consumer aggregating them must not double-count the plan.
func TestReportInFlightIsPrePlan(t *testing.T) {
	now := time.Now().UTC()
	running := item("WLM-9", domain.StatusInProgress, 0, now)
	running.SchedulingState = domain.SchedulingRunning
	rep := Plan(Input{
		Now:   now,
		Items: []*domain.WorkItem{running, item("WLM-1", domain.StatusReady, 5, now)},
		Caps:  Caps{Global: 5},
	})
	if rep.InFlight != 1 || rep.InFlightByStatus[domain.StatusInProgress] != 1 {
		t.Fatalf("in flight = %d %v, want just the running item", rep.InFlight, rep.InFlightByStatus)
	}
	if _, ok := rep.InFlightByStatus[domain.StatusReady]; ok {
		t.Fatalf("in-flight by status counts a planned start: %v", rep.InFlightByStatus)
	}
	if len(rep.Start) != 1 {
		t.Fatalf("start = %v, want the ready item", rep.Start)
	}
}
