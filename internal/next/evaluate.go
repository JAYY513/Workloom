// Package next evaluates a project's readiness verdict and the single
// recommended next action (方案 §7.4, 实施计划 M3.4). Evaluation is a pure
// function over observed state so it stays deterministic and testable; the
// CLI collects the inputs read-only.
//
// The output never carries time estimates: risks and recommendations are
// observations and orders, not durations.
package next

import (
	"fmt"
	"sort"
	"time"

	"workloom/internal/domain"
	"workloom/internal/reconcile"
	"workloom/internal/storage"
)

// Verdicts (方案 §7.4): only these three outcomes exist.
const (
	VerdictPass     = "PASS"
	VerdictConcerns = "CONCERNS"
	VerdictFail     = "FAIL"
)

// Recommendation actions, in the fixed §7.4 priority order.
const (
	ActionRecoverClaim    = "recover_claim"
	ActionReview          = "review"
	ActionStart           = "start"
	ActionStartBacklog    = "start_backlog"
	ActionMilestoneReview = "milestone_review"
	ActionReportDone      = "report_done"
)

// Risk kinds.
const (
	RiskExpiredLease      = "expired_lease"
	RiskOrphanClaim       = "orphan_claim"
	RiskUnreadableLease   = "unreadable_lease"
	RiskStaleReview       = "stale_review"
	RiskBlocked           = "blocked"
	RiskInvalidMetadata   = "invalid_metadata"
	RiskInvalidPolicy     = "invalid_policy"
	RiskPendingApproval   = "pending_approval"
	RiskInspectionLimited = "inspection_limited"
)

// ReviewStaleAfter is how long a work item may wait in review/verification
// before it is reported as stale.
const ReviewStaleAfter = 24 * time.Hour

// defaultRecoverCommand is the FAIL remediation hint when Input carries none.
const defaultRecoverCommand = `devsys recover --actor operator --reason "recover interrupted state"`

// Input is the observed project stateEvaluate consumes.
type Input struct {
	Now              time.Time
	WorkItems        []*domain.WorkItem
	Milestones       []domain.Milestone
	PendingTxns      []storage.PendingTxn
	ExpiredLeases    []reconcile.LeaseProbe
	OrphanLeases     []reconcile.LeaseProbe
	UnreadableLeases []reconcile.LeaseProbe
	InspectionOK     bool
	InspectionNote   string
	MetadataProblems []string
	PolicyProblems   []string
	// PendingApprovals are approvals waiting for a decision; the work items
	// behind them join the "waiting for review" priority class (§7.4 优先级 2).
	PendingApprovals []PendingApproval
	RecoverCommand   string
}

// PendingApproval names one approval waiting for a decision.
type PendingApproval struct {
	ID         string `json:"id"`
	WorkitemID string `json:"workitem_id"`
}

// Risk is one recorded risk signal.
type Risk struct {
	Kind       string `json:"kind"`
	WorkitemID string `json:"workitem_id,omitempty"`
	Detail     string `json:"detail"`
}

// Fix is one FAIL remediation, ordered by severity.
type Fix struct {
	Reason   string `json:"reason"`
	Command  string `json:"command"`
	Workflow string `json:"workflow,omitempty"`
}

// Recommendation is the single next action.
type Recommendation struct {
	Action      string `json:"action"`
	WorkitemID  string `json:"workitem_id,omitempty"`
	MilestoneID string `json:"milestone_id,omitempty"`
	Reason      string `json:"reason"`
}

// Report is the readiness verdict with its risks, fixes and next action.
type Report struct {
	Verdict string         `json:"verdict"`
	Reasons []string       `json:"reasons"`
	Risks   []Risk         `json:"risks"`
	Fixes   []Fix          `json:"fixes,omitempty"`
	Next    Recommendation `json:"next"`
}

// Evaluate applies §7.4: FAIL while pending transactions make the state
// untrustworthy, CONCERNS with any recorded risk signal, PASS otherwise;
// the recommendation follows the fixed priority ladder.
func Evaluate(in Input) Report {
	rep := Report{Risks: []Risk{}, Reasons: []string{}}

	rep.Risks = append(rep.Risks, leaseRisks(RiskExpiredLease, in.ExpiredLeases, "lease expired")...)
	rep.Risks = append(rep.Risks, leaseRisks(RiskOrphanClaim, in.OrphanLeases, "claim has no run file")...)
	rep.Risks = append(rep.Risks, leaseRisks(RiskUnreadableLease, in.UnreadableLeases, "scheduling file unreadable")...)

	for _, wi := range sortedWorkItems(in.WorkItems, orderByUpdated) {
		if isReviewFamily(wi.Status) && !wi.UpdatedAt.After(in.Now.Add(-ReviewStaleAfter)) {
			rep.Risks = append(rep.Risks, Risk{
				Kind: RiskStaleReview, WorkitemID: wi.ID,
				Detail: fmt.Sprintf("%s since %s", wi.Status, wi.UpdatedAt.UTC().Format(time.RFC3339)),
			})
		}
	}
	for _, wi := range sortedWorkItems(in.WorkItems, orderByID) {
		if wi.Status == domain.StatusBlocked {
			detail := "blocked"
			if wi.PreviousStatus != "" {
				detail = "blocked (previous: " + wi.PreviousStatus + ")"
			}
			rep.Risks = append(rep.Risks, Risk{Kind: RiskBlocked, WorkitemID: wi.ID, Detail: detail})
		}
	}
	for _, p := range in.MetadataProblems {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskInvalidMetadata, Detail: p})
	}
	for _, p := range in.PolicyProblems {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskInvalidPolicy, Detail: p})
	}
	pending := append([]PendingApproval(nil), in.PendingApprovals...)
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
	for _, p := range pending {
		rep.Risks = append(rep.Risks, Risk{
			Kind: RiskPendingApproval, WorkitemID: p.WorkitemID,
			Detail: fmt.Sprintf("%s is waiting for a decision", p.ID),
		})
	}
	if !in.InspectionOK && len(in.PendingTxns) == 0 {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskInspectionLimited, Detail: in.InspectionNote})
	}

	switch {
	case len(in.PendingTxns) > 0:
		rep.Verdict = VerdictFail
		rep.Reasons = append(rep.Reasons,
			fmt.Sprintf("%d pending transaction(s) block trustworthy state", len(in.PendingTxns)))
		command := in.RecoverCommand
		if command == "" {
			command = defaultRecoverCommand
		}
		rep.Fixes = append(rep.Fixes, Fix{
			Reason:  "recover interrupted transactions before trusting or advancing state",
			Command: command,
		})
	case len(rep.Risks) > 0:
		rep.Verdict = VerdictConcerns
		rep.Reasons = append(rep.Reasons, fmt.Sprintf("%d risk signal(s) recorded", len(rep.Risks)))
	default:
		rep.Verdict = VerdictPass
		rep.Reasons = append(rep.Reasons, "no risk signals recorded")
	}

	rep.Next = recommend(in)
	return rep
}

// recommend walks the §7.4 ladder top-down and returns the first hit.
func recommend(in Input) Recommendation {
	probes := append(append([]reconcile.LeaseProbe{}, in.ExpiredLeases...), in.OrphanLeases...)
	if len(probes) > 0 {
		sort.SliceStable(probes, func(i, j int) bool { return probes[i].WorkitemID < probes[j].WorkitemID })
		id := probes[0].WorkitemID
		return Recommendation{
			Action: ActionRecoverClaim, WorkitemID: id,
			Reason: "claim is expired or orphaned; recover it before dispatching",
		}
	}
	pendingByWorkitem := map[string]string{}
	for _, p := range in.PendingApprovals {
		pendingByWorkitem[p.WorkitemID] = p.ID
	}
	waitingForReview := func(wi *domain.WorkItem) bool {
		return isReviewFamily(wi.Status) || pendingByWorkitem[wi.ID] != ""
	}
	if wi := firstWhere(in.WorkItems, waitingForReview, orderByUpdated); wi != nil {
		reason := fmt.Sprintf("waiting in %s", wi.Status)
		if id := pendingByWorkitem[wi.ID]; id != "" {
			reason = fmt.Sprintf("waiting for %s", id)
		}
		return Recommendation{Action: ActionReview, WorkitemID: wi.ID, Reason: reason}
	}
	if wi := firstWhere(in.WorkItems, func(wi *domain.WorkItem) bool {
		return wi.Status == domain.StatusReady
	}, orderDispatch); wi != nil {
		return Recommendation{Action: ActionStart, WorkitemID: wi.ID, Reason: "highest-priority ready task"}
	}
	if wi := firstWhere(in.WorkItems, func(wi *domain.WorkItem) bool {
		return wi.Status == domain.StatusBacklog
	}, orderCreated); wi != nil {
		return Recommendation{
			Action: ActionStartBacklog, WorkitemID: wi.ID,
			Reason: "oldest backlog task; promote it to ready first",
		}
	}
	for _, m := range in.Milestones {
		if !milestoneDone(m.Status) {
			return Recommendation{
				Action: ActionMilestoneReview, MilestoneID: m.ID,
				Reason: fmt.Sprintf("milestone %q is not done", milestoneName(m)),
			}
		}
	}
	return Recommendation{Action: ActionReportDone, Reason: "no runnable work item remains"}
}

func isReviewFamily(status string) bool {
	return status == domain.StatusReview || status == domain.StatusVerification
}

// milestoneDone treats "done" and "completed" as finished milestone states.
func milestoneDone(status string) bool {
	return status == "done" || status == "completed"
}

func milestoneName(m domain.Milestone) string {
	if m.Name != "" {
		return m.Name
	}
	if m.ID != "" {
		return m.ID
	}
	return "<unnamed>"
}

func leaseRisks(kind string, probes []reconcile.LeaseProbe, detail string) []Risk {
	sorted := append([]reconcile.LeaseProbe(nil), probes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].WorkitemID < sorted[j].WorkitemID })
	out := make([]Risk, 0, len(sorted))
	for _, p := range sorted {
		d := detail
		if !p.LeaseUntil.IsZero() {
			d = fmt.Sprintf("%s (lease until %s)", detail, p.LeaseUntil.UTC().Format(time.RFC3339))
		}
		out = append(out, Risk{Kind: kind, WorkitemID: p.WorkitemID, Detail: d})
	}
	return out
}

func firstWhere(items []*domain.WorkItem, match func(*domain.WorkItem) bool, less func(a, b *domain.WorkItem) bool) *domain.WorkItem {
	candidates := make([]*domain.WorkItem, 0, len(items))
	for _, wi := range items {
		if wi != nil && match(wi) {
			candidates = append(candidates, wi)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return less(candidates[i], candidates[j]) })
	return candidates[0]
}

func sortedWorkItems(items []*domain.WorkItem, less func(a, b *domain.WorkItem) bool) []*domain.WorkItem {
	out := make([]*domain.WorkItem, 0, len(items))
	for _, wi := range items {
		if wi != nil {
			out = append(out, wi)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// orderDispatch is the §7.4 dispatch order: priority descending, then oldest
// creation, then identifier.
func orderDispatch(a, b *domain.WorkItem) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

func orderCreated(a, b *domain.WorkItem) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

func orderByUpdated(a, b *domain.WorkItem) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.Before(b.UpdatedAt)
	}
	return a.ID < b.ID
}

func orderByID(a, b *domain.WorkItem) bool { return a.ID < b.ID }
