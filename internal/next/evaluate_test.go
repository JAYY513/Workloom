package next

import (
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/reconcile"
	"workloom/internal/storage"
)

var t0 = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func wi(id, status string, priority int, created, updated time.Time) *domain.WorkItem {
	return &domain.WorkItem{ID: id, Status: status, Priority: priority, CreatedAt: created, UpdatedAt: updated}
}

func kinds(rep Report) map[string]bool {
	out := map[string]bool{}
	for _, r := range rep.Risks {
		out[r.Kind] = true
	}
	return out
}

func TestVerdictPassAndReportDone(t *testing.T) {
	rep := Evaluate(Input{Now: t0, InspectionOK: true})
	if rep.Verdict != VerdictPass || len(rep.Risks) != 0 {
		t.Fatalf("rep = %+v", rep)
	}
	if rep.Next.Action != ActionReportDone {
		t.Errorf("next = %+v", rep.Next)
	}
}

func TestVerdictFailListsRecoveryFix(t *testing.T) {
	rep := Evaluate(Input{Now: t0, InspectionOK: true, PendingTxns: []storage.PendingTxn{{ID: "tx-1"}}})
	if rep.Verdict != VerdictFail {
		t.Fatalf("verdict = %s", rep.Verdict)
	}
	if len(rep.Fixes) != 1 || !strings.Contains(rep.Fixes[0].Command, "recover") {
		t.Fatalf("fixes = %+v", rep.Fixes)
	}
}

func TestRisksCoverAllFourClasses(t *testing.T) {
	in := Input{
		Now:          t0,
		InspectionOK: true,
		ExpiredLeases: []reconcile.LeaseProbe{
			{WorkitemID: "WLM-2", LeaseUntil: t0.Add(-time.Hour)},
		},
		OrphanLeases: []reconcile.LeaseProbe{
			{WorkitemID: "WLM-3", LeaseUntil: t0.Add(time.Hour)},
		},
		WorkItems: []*domain.WorkItem{
			wi("WLM-4", domain.StatusBlocked, 0, t0.Add(-4*time.Hour), t0.Add(-3*time.Hour)),
			wi("WLM-5", domain.StatusReview, 0, t0.Add(-50*time.Hour), t0.Add(-40*time.Hour)),
			wi("WLM-6", domain.StatusReady, 0, t0.Add(-time.Hour), t0.Add(-time.Hour)),
		},
	}
	in.WorkItems[0].PreviousStatus = domain.StatusReady

	rep := Evaluate(in)
	if rep.Verdict != VerdictConcerns {
		t.Fatalf("verdict = %s", rep.Verdict)
	}
	got := kinds(rep)
	for _, want := range []string{RiskExpiredLease, RiskOrphanClaim, RiskBlocked, RiskStaleReview} {
		if !got[want] {
			t.Errorf("missing risk %q in %+v", want, rep.Risks)
		}
	}
	if rep.Risks[0].Kind != RiskExpiredLease || rep.Risks[1].Kind != RiskOrphanClaim {
		t.Errorf("risk order = %+v", rep.Risks)
	}
	if rep.Next.Action != ActionRecoverClaim || rep.Next.WorkitemID != "WLM-2" {
		t.Errorf("next = %+v", rep.Next)
	}
	var blockedDetail string
	for _, r := range rep.Risks {
		if r.Kind == RiskBlocked {
			blockedDetail = r.Detail
		}
	}
	if !strings.Contains(blockedDetail, "previous: ready") {
		t.Errorf("blocked detail = %q", blockedDetail)
	}
}

func TestAuxiliaryRisks(t *testing.T) {
	rep := Evaluate(Input{
		Now:              t0,
		InspectionOK:     false,
		InspectionNote:   "inspection unavailable: no lock file",
		UnreadableLeases: []reconcile.LeaseProbe{{WorkitemID: "WLM-9"}},
		MetadataProblems: []string{"project.yaml:2: id: required"},
	})
	got := kinds(rep)
	for _, want := range []string{RiskUnreadableLease, RiskInvalidMetadata, RiskInspectionLimited} {
		if !got[want] {
			t.Errorf("missing risk %q in %+v", want, rep.Risks)
		}
	}
	if rep.Verdict != VerdictConcerns {
		t.Errorf("verdict = %s", rep.Verdict)
	}
}

func TestNextPriorityLadder(t *testing.T) {
	ready := wi("WLM-10", domain.StatusReady, 1, t0.Add(-time.Hour), t0.Add(-time.Hour))
	review := wi("WLM-11", domain.StatusReview, 0, t0.Add(-2*time.Hour), t0.Add(-2*time.Hour))
	backlog := wi("WLM-12", domain.StatusBacklog, 99, t0.Add(-3*time.Hour), t0.Add(-3*time.Hour))

	cases := []struct {
		name string
		in   Input
		want string
		id   string
	}{
		{"review before ready", Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{ready, review}}, ActionReview, "WLM-11"},
		{"ready before backlog", Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{ready, backlog}}, ActionStart, "WLM-10"},
		{"backlog last", Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{backlog}}, ActionStartBacklog, "WLM-12"},
		{"milestone review", Input{Now: t0, InspectionOK: true, Milestones: []domain.Milestone{{ID: "M1", Name: "M1", Status: "pending"}}}, ActionMilestoneReview, ""},
		{"report done", Input{Now: t0, InspectionOK: true, Milestones: []domain.Milestone{{ID: "M1", Status: "done"}}}, ActionReportDone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := Evaluate(tc.in)
			if rep.Next.Action != tc.want {
				t.Fatalf("action = %s, want %s (%+v)", rep.Next.Action, tc.want, rep.Next)
			}
			if tc.id != "" && rep.Next.WorkitemID != tc.id {
				t.Errorf("workitem = %s, want %s", rep.Next.WorkitemID, tc.id)
			}
		})
	}
}

func TestBlockedWorkItemsAreNotCandidates(t *testing.T) {
	blocked := wi("WLM-1", domain.StatusBlocked, 99, t0.Add(-time.Hour), t0.Add(-time.Hour))
	backlog := wi("WLM-2", domain.StatusBacklog, 0, t0.Add(-time.Hour), t0.Add(-time.Hour))
	rep := Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{blocked, backlog}})
	if rep.Next.Action != ActionStartBacklog || rep.Next.WorkitemID != "WLM-2" {
		t.Fatalf("next = %+v", rep.Next)
	}
}

func TestStartDispatchOrder(t *testing.T) {
	base := t0.Add(-time.Hour)
	items := []*domain.WorkItem{
		wi("WLM-3", domain.StatusReady, 5, base, base),
		wi("WLM-1", domain.StatusReady, 5, base.Add(-time.Minute), base),
		wi("WLM-2", domain.StatusReady, 9, base.Add(time.Minute), base),
	}
	if got := Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: items}).Next.WorkitemID; got != "WLM-2" {
		t.Fatalf("priority order picked %s, want WLM-2", got)
	}
	items[2].Priority = 5
	if got := Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: items}).Next.WorkitemID; got != "WLM-1" {
		t.Fatalf("created order picked %s, want WLM-1", got)
	}
	items[0].CreatedAt = base.Add(-time.Minute)
	if got := Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: items}).Next.WorkitemID; got != "WLM-1" {
		t.Fatalf("id order picked %s, want WLM-1", got)
	}
}

func TestPendingApprovalRiskAndPriority(t *testing.T) {
	ready := wi("WLM-9", domain.StatusReady, 1, t0.Add(-time.Hour), t0.Add(-time.Hour))
	other := wi("WLM-1", domain.StatusReady, 50, t0.Add(-2*time.Hour), t0.Add(-2*time.Hour))
	rep := Evaluate(Input{
		Now: t0, InspectionOK: true,
		WorkItems:        []*domain.WorkItem{ready, other},
		PendingApprovals: []PendingApproval{{ID: "approval-2", WorkitemID: "WLM-9"}},
	})
	if rep.Verdict != VerdictConcerns {
		t.Fatalf("verdict = %s", rep.Verdict)
	}
	if !kinds(rep)[RiskPendingApproval] {
		t.Fatalf("missing pending_approval risk: %+v", rep.Risks)
	}
	// A pending approval lifts its work item into the waiting class, ahead
	// of a higher-priority ready task.
	if rep.Next.Action != ActionReview || rep.Next.WorkitemID != "WLM-9" || rep.Next.Reason != "waiting for approval-2" {
		t.Fatalf("next = %+v", rep.Next)
	}

	// Risk entries are ordered by approval id.
	rep = Evaluate(Input{
		Now: t0, InspectionOK: true,
		PendingApprovals: []PendingApproval{
			{ID: "approval-3", WorkitemID: "WLM-3"},
			{ID: "approval-1", WorkitemID: "WLM-1"},
		},
	})
	if rep.Risks[0].Kind != RiskPendingApproval || rep.Risks[0].WorkitemID != "WLM-1" || rep.Risks[1].WorkitemID != "WLM-3" {
		t.Fatalf("risks = %+v", rep.Risks)
	}
}

func TestStaleReviewBoundary(t *testing.T) {
	at := t0.Add(-ReviewStaleAfter) // exactly 24h counts as stale
	rep := Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{
		wi("WLM-1", domain.StatusReview, 0, at, at),
	}})
	if !kinds(rep)[RiskStaleReview] {
		t.Errorf("exactly 24h should be reported stale: %+v", rep.Risks)
	}

	fresh := t0.Add(-ReviewStaleAfter + time.Second)
	rep = Evaluate(Input{Now: t0, InspectionOK: true, WorkItems: []*domain.WorkItem{
		wi("WLM-1", domain.StatusVerification, 0, fresh, fresh),
	}})
	if kinds(rep)[RiskStaleReview] {
		t.Errorf("fresh verification flagged stale: %+v", rep.Risks)
	}
}
