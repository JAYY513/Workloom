package app

import (
	"context"
	"fmt"
	"strings"

	"workloom/internal/approval"
	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/next"
	"workloom/internal/reconcile"
	"workloom/internal/workflow"
)

// Next evaluates the readiness verdict and the single recommended next
// action (方案 §7.4, 实施计划 M3.4). Like doctor it never writes, recovers or
// creates the local lock; while pending transactions exist — or no writer
// ever ran — it reports without rendering business facts (方案 §15.4).
//
// The returned work items are the facts the evaluation consumed; they are
// empty whenever the inspection is not trustworthy.
func (s *Service) Next(ctx context.Context) (next.Report, []*domain.WorkItem, error) {
	doc, err := reconcile.Doctor(ctx, s.Root, reconcile.Options{})
	if err != nil {
		return next.Report{}, nil, Internalf("next: inspect: %v", err)
	}
	in := next.Input{
		Now:              s.now(),
		PendingTxns:      doc.PendingTransactions,
		ExpiredLeases:    doc.ExpiredLeases,
		OrphanLeases:     doc.OrphanLeases,
		UnreadableLeases: doc.UnreadableLeases,
		InspectionOK:     doc.InspectionOK,
		InspectionNote:   doc.Note,
		RecoverCommand:   `devsys recover --actor operator --reason "recover interrupted state"`,
	}
	var items []*domain.WorkItem
	if doc.InspectionOK && len(doc.PendingTransactions) == 0 {
		items, err = s.items().List(ctx)
		if err != nil {
			return next.Report{}, nil, Internalf("next: list work items: %v", err)
		}
		in.WorkItems = items
		pending, err := approval.New(s.Root).List(ctx, approval.Filter{Status: approval.StatusPending})
		if err != nil {
			return next.Report{}, nil, Internalf("next: list approvals: %v", err)
		}
		for _, a := range pending {
			if a.InvalidatedAt != nil {
				continue
			}
			in.PendingApprovals = append(in.PendingApprovals, next.PendingApproval{ID: a.ID, WorkitemID: a.WorkItemID})
		}
	}
	md, problems := config.Diagnose(s.Root)
	for _, p := range problems {
		in.MetadataProblems = append(in.MetadataProblems, p.String())
	}
	if md != nil && md.Project != nil {
		in.Milestones = md.Project.Milestones
	}
	// Invalid or missing policy files are recorded risks; the last-known-good
	// fallback keeps read paths usable while dispatch stays blocked.
	policies := workflow.Load(s.Root)
	present := map[string]bool{}
	for _, res := range policies {
		name := res.File
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		present[strings.TrimSuffix(name, ".md")] = true
		for _, is := range res.Issues {
			if is.Severity == workflow.SeverityError {
				in.PolicyProblems = append(in.PolicyProblems, is.String())
			}
		}
	}
	for _, wi := range in.WorkItems {
		if wi.Workflow == nil || wi.Workflow.ID == "" || present[wi.Workflow.ID] {
			continue
		}
		in.PolicyProblems = append(in.PolicyProblems,
			fmt.Sprintf("work item %s references missing policy %q", wi.ID, wi.Workflow.ID))
	}
	return next.Evaluate(in), items, nil
}
