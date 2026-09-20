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
	if md != nil && md.Config != nil {
		in.DefaultPolicy = strings.TrimSpace(md.Config.DefaultPolicy)
	}
	// Invalid or missing policy files are recorded risks; the last-known-good
	// fallback keeps read paths usable while dispatch stays blocked.
	policies := workflow.Load(s.Root)
	present := map[string]bool{}
	parsed := map[string]*workflow.Policy{}
	for _, res := range policies {
		name := res.File
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		id := strings.TrimSuffix(name, ".md")
		present[id] = true
		if res.Policy != nil {
			parsed[id] = res.Policy
		}
		for _, is := range res.Issues {
			if is.Severity == workflow.SeverityError {
				in.PolicyProblems = append(in.PolicyProblems, is.String())
			}
		}
	}
	in.PolicyIDs = policyIDs(policies)
	if in.DefaultPolicy != "" && !present[in.DefaultPolicy] {
		in.PolicyProblems = append(in.PolicyProblems,
			fmt.Sprintf("config.yaml default_policy %q has no .devsys/workflows/%s.md", in.DefaultPolicy, in.DefaultPolicy))
	}
	for _, wi := range in.WorkItems {
		if wi.Workflow != nil && wi.Workflow.ID != "" && !present[wi.Workflow.ID] {
			in.PolicyProblems = append(in.PolicyProblems,
				fmt.Sprintf("work item %s references missing policy %q", wi.ID, wi.Workflow.ID))
		}
	}
	// Same judgement `workitem claim` applies (§4.7 领取质量门): a ready task the
	// gate would reject is a risk signal, never a clean PASS. The policy bodies
	// come from the plain reads above — the last-known-good resolver refreshes a
	// cache, and `next` must not write. A file that failed to parse is already a
	// risk, and a claim under it is refused before the quality gate runs.
	in.QualityBlocks = next.QualityBlocks(in.WorkItems, func(wi *domain.WorkItem) (string, string, *workflow.Policy) {
		id := in.DefaultPolicy
		if wi.Workflow != nil && wi.Workflow.ID != "" {
			id = wi.Workflow.ID
		}
		if id == "" {
			return "", "", nil
		}
		policy := parsed[id]
		if policy == nil {
			return "", "", nil
		}
		return id, policy.File, policy
	})
	return next.Evaluate(in), items, nil
}
