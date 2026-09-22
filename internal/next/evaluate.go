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
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/reconcile"
	"github.com/JAYY513/Workloom/internal/storage"
	"github.com/JAYY513/Workloom/internal/workflow"
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
	RiskQualityBlocked    = "quality_blocked"
	RiskRetryPending      = "retry_pending"
	RiskInvalidMetadata   = "invalid_metadata"
	RiskInvalidPolicy     = "invalid_policy"
	RiskPendingApproval   = "pending_approval"
	RiskInspectionLimited = "inspection_limited"
	RiskEmptyProject      = "empty_project"
	RiskDeadAttempt       = "dead_attempt"
	RiskInFlight          = "in_flight"
)

// ReviewStaleAfter is how long a work item may wait in review/verification
// before it is reported as stale.
const ReviewStaleAfter = 24 * time.Hour

// defaultRecoverCommand is the FAIL remediation hint when Input carries none.
const defaultRecoverCommand = `workloom recover --actor operator --reason "recover interrupted state"`

// CreateWorkitemCommand is the empty-project remediation `next` and `init`
// name, so the first-use path does not invent a second create recipe.
const CreateWorkitemCommand = `workloom workitem create --title "…" --actor <you> --reason "first task"`

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
	// QualityBlocks are the ready work items whose claim the effective policy's
	// quality gate would refuse (§4.7 领取质量门). The application layer derives
	// them with the same judgement claim applies, so a PASS verdict never
	// recommends a claim that is certain to fail.
	QualityBlocks []QualityBlock
	// DefaultPolicy is the project-level policy (.devsys/config.yaml) governing
	// work items that declare no instance; PolicyIDs are the policies the
	// project declares. Together they tell whether a recommended work item
	// would run with no gate at all.
	DefaultPolicy  string
	PolicyIDs      []string
	RecoverCommand string
	// HasBlueprint is true when the project declares a blueprint artifact.
	// It is only consulted for the empty-project risk, so a project with
	// work and no blueprint can still PASS.
	HasBlueprint bool
	// DeadAttempts are in-progress claims whose latest run has already
	// failed or produced no evidence; they occupy §7.4 rung 1 (recover_claim).
	DeadAttempts []AttemptRef
	// InFlight are healthy attempts still executing. They are a CONCERNS
	// signal, not a new ladder rung: recommend may still land on report_done.
	InFlight []AttemptRef
}

// QualityBlock is a ready work item whose claim the quality gate would refuse.
type QualityBlock struct {
	WorkitemID string `json:"workitem_id"`
	PolicyID   string `json:"policy_id"`
	PolicyFile string `json:"policy_file"`
	Score      int    `json:"score"`
	MinScore   int    `json:"min_score"`
	// Improvements are the scored-zero components the caller must fix; they
	// mirror the claim rejection's problem list.
	Improvements []string `json:"improvements,omitempty"`
}

// PolicySource reports the policy governing one work item: its id ("" when no
// policy governs the item), the file it was read from, and the parsed policy
// (nil when the file could not be parsed).
type PolicySource func(wi *domain.WorkItem) (id, file string, policy *workflow.Policy)

// QualityBlocks returns the ready work items whose claim the quality gate
// would refuse, scored with the same deterministic judgement `workitem claim`
// applies (§4.7 领取质量门). Callers pass the effective policy per item, so an
// item guarded by the project default_policy is judged exactly like one with
// its own instance.
func QualityBlocks(items []*domain.WorkItem, source PolicySource) []QualityBlock {
	var out []QualityBlock
	for _, wi := range sortedWorkItems(items, orderByID) {
		if wi.Status != domain.StatusReady {
			continue
		}
		id, file, policy := source(wi)
		if policy == nil {
			continue // no policy governs the item: nothing gates the claim
		}
		quality := workflow.ScoreQuality(workflow.QualityInput{
			Title:              wi.Title,
			Description:        wi.Description,
			AcceptanceCriteria: wi.AcceptanceCriteria,
		})
		if !policy.QualityGateBlocks(quality) {
			continue
		}
		out = append(out, QualityBlock{
			WorkitemID: wi.ID, PolicyID: id, PolicyFile: file,
			Score: quality.Score, MinScore: policy.QualityGate.MinScore,
			Improvements: quality.Improvements,
		})
	}
	return out
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
	// A ready work item the quality gate would reject is a risk signal, not a
	// silent PASS: the verdict and the recommendation must agree with what
	// `workitem claim` will answer.
	for _, qb := range in.QualityBlocks {
		rep.Risks = append(rep.Risks, Risk{
			Kind: RiskQualityBlocked, WorkitemID: qb.WorkitemID,
			Detail: fmt.Sprintf("claim would fail the quality gate of %s: score %d is below min_score %d",
				qb.policyName(), qb.Score, qb.MinScore),
		})
	}
	for _, wi := range retryQueue(in) {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskRetryPending, WorkitemID: wi.ID, Detail: retryDetail(wi, in.Now)})
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
	if in.emptyProject() {
		detail := "no work items; create one with `" + CreateWorkitemCommand + "`"
		if !in.HasBlueprint {
			detail += "; no blueprint declared (declare one: workloom project update --blueprint-artifact <artifact-id>)"
		}
		rep.Risks = append(rep.Risks, Risk{Kind: RiskEmptyProject, Detail: detail})
	}
	for _, a := range sortedAttemptRefs(in.DeadAttempts) {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskDeadAttempt, WorkitemID: a.WorkitemID, Detail: a.Detail})
	}
	for _, a := range sortedAttemptRefs(in.InFlight) {
		rep.Risks = append(rep.Risks, Risk{Kind: RiskInFlight, WorkitemID: a.WorkitemID, Detail: a.Detail})
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
	if dead := sortedAttemptRefs(in.DeadAttempts); len(dead) > 0 {
		return Recommendation{
			Action: ActionRecoverClaim, WorkitemID: dead[0].WorkitemID,
			Reason: dead[0].Detail,
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
		reason := "highest-priority ready task"
		if qb, ok := in.qualityBlock(wi.ID); ok {
			// The ladder still points at the task to work on; the reason says
			// what to do first, because claiming it as-is is a known failure.
			reason = fmt.Sprintf("ready, but the quality gate would refuse the claim (score %d is below min_score %d): %s; fix it first with `workloom workitem update --id %s`",
				qb.Score, qb.MinScore, strings.Join(qb.Improvements, "；"), wi.ID)
		}
		return Recommendation{Action: ActionStart, WorkitemID: wi.ID, Reason: reason + in.policyGap(wi)}
	}
	if wi := firstWhere(in.WorkItems, func(wi *domain.WorkItem) bool {
		return wi.Status == domain.StatusBacklog
	}, orderCreated); wi != nil {
		return Recommendation{
			Action: ActionStartBacklog, WorkitemID: wi.ID,
			Reason: "oldest backlog task; promote it to ready first" + in.policyGap(wi),
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
	reason := "no runnable work item remains"
	if in.emptyProject() {
		reason = "no work items yet; create one with `" + CreateWorkitemCommand + "`"
	}
	if queue := retryQueue(in); len(queue) > 0 {
		// Idle is wrong while a retry waits: the next scheduling tick is the
		// work, and it will not happen on its own (§4.8).
		reason = fmt.Sprintf("%s; %d work item(s) wait for a dispatch retry (%s) — run `workloom dispatch --once`",
			reason, len(queue), retrySchedule(queue))
	}
	if inflight := sortedAttemptRefs(in.InFlight); len(inflight) > 0 {
		reason = fmt.Sprintf("%s; %d attempt(s) still in flight (%s) — wait or inspect `workloom run get --id %s`",
			reason, len(inflight), inflightIDs(inflight), inflight[0].RunID)
	}
	return Recommendation{Action: ActionReportDone, Reason: reason}
}

func (in Input) emptyProject() bool {
	return in.InspectionOK && len(in.PendingTxns) == 0 && len(in.WorkItems) == 0
}

// qualityBlock returns the recorded quality-gate block for a work item.
func (in Input) qualityBlock(id string) (QualityBlock, bool) {
	for _, qb := range in.QualityBlocks {
		if qb.WorkitemID == id {
			return qb, true
		}
	}
	return QualityBlock{}, false
}

// policyGap tells the reader that a recommended work item is under no policy
// at all while the project declares policies and sets no default: the quality
// gate and the stage gates did not run for it. Work items the project never
// intended to gate (no policy files at all) stay unannotated.
func (in Input) policyGap(wi *domain.WorkItem) string {
	if len(in.PolicyIDs) == 0 || in.DefaultPolicy != "" {
		return ""
	}
	if wi.Workflow != nil && wi.Workflow.ID != "" {
		return ""
	}
	return fmt.Sprintf("; no workflow policy is bound to %s and config.yaml declares no default_policy, so quality and stage gates are not enforced", wi.ID)
}

// policyName names the policy behind a quality block for humans.
func (qb QualityBlock) policyName() string {
	if qb.PolicyID != "" {
		return qb.PolicyID
	}
	return "the workflow policy"
}

// retryQueue lists retry_queued work items, soonest attempt first.
func retryQueue(in Input) []*domain.WorkItem {
	var out []*domain.WorkItem
	for _, wi := range in.WorkItems {
		if wi != nil && wi.Status == domain.StatusRetryQueued {
			out = append(out, wi)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return orderRetry(out[i], out[j]) })
	return out
}

func orderRetry(a, b *domain.WorkItem) bool {
	switch {
	case a.NextAttemptAt == nil && b.NextAttemptAt == nil:
		return a.ID < b.ID
	case a.NextAttemptAt == nil:
		return false
	case b.NextAttemptAt == nil:
		return true
	case !a.NextAttemptAt.Equal(*b.NextAttemptAt):
		return a.NextAttemptAt.Before(*b.NextAttemptAt)
	}
	return a.ID < b.ID
}

// retryDetail describes one queued retry. next_attempt_at is a recorded fact,
// not an estimate, so naming it keeps §7.4's "no time estimates" rule.
func retryDetail(wi *domain.WorkItem, now time.Time) string {
	if wi.NextAttemptAt == nil {
		return "retry queued; no next_attempt_at recorded"
	}
	at := wi.NextAttemptAt.UTC()
	if at.After(now) {
		return fmt.Sprintf("retry queued; next attempt at %s", at.Format(time.RFC3339))
	}
	return fmt.Sprintf("retry queued and due since %s; run `workloom dispatch --once`", at.Format(time.RFC3339))
}

// retrySchedule summarizes a retry queue for a recommendation reason.
func retrySchedule(queue []*domain.WorkItem) string {
	if queue[0].NextAttemptAt == nil {
		return "no next_attempt_at recorded"
	}
	return "next attempt at " + queue[0].NextAttemptAt.UTC().Format(time.RFC3339)
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
