package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/JAYY513/Workloom/internal/approval"
	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/next"
	"github.com/JAYY513/Workloom/internal/reconcile"
	"github.com/JAYY513/Workloom/internal/run"
	"github.com/JAYY513/Workloom/internal/workflow"
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
	if len(doc.InvalidFiles) > 0 {
		// A verdict rendered from an incomplete work item list could
		// recommend the wrong action, so this is untrusted state (exit 4),
		// not a verdict with a footnote (方案 §14.1).
		names := make([]string, 0, len(doc.InvalidFiles))
		for _, f := range doc.InvalidFiles {
			names = append(names, f.String())
		}
		shown := names
		if len(names) > 3 {
			shown = names[:3]
		}
		msg := "next: managed state unreadable: " + strings.Join(shown, "; ")
		if extra := len(names) - len(shown); extra > 0 {
			msg += fmt.Sprintf("; (+%d more)", extra)
		}
		return next.Report{}, nil, Invalidf(KindInvalid, nil, "%s", msg)
	}
	in := next.Input{
		Now:              s.now(),
		PendingTxns:      doc.PendingTransactions,
		ExpiredLeases:    doc.ExpiredLeases,
		OrphanLeases:     doc.OrphanLeases,
		UnreadableLeases: doc.UnreadableLeases,
		InspectionOK:     doc.InspectionOK,
		InspectionNote:   doc.Note,
		RecoverCommand:   `workloom recover --actor operator --reason "recover interrupted state"`,
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
		in.HasBlueprint = strings.TrimSpace(md.Project.BlueprintArtifactID) != ""
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
	if len(in.WorkItems) > 0 {
		dead, inflight, err := s.attemptSignals(ctx, in.WorkItems)
		if err != nil {
			return next.Report{}, nil, err
		}
		in.DeadAttempts, in.InFlight = dead, inflight
	}
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

// attemptSignals classifies in-progress claims for `next`: a failed or empty
// attempt is a dead_attempt (rung 1), a healthy running one is in_flight.
func (s *Service) attemptSignals(ctx context.Context, items []*domain.WorkItem) (dead, inflight []next.AttemptRef, err error) {
	runs, err := run.New(s.Root).List(ctx)
	if err != nil {
		return nil, nil, Internalf("next: list runs: %v", err)
	}
	latest := map[string]*domain.Run{}
	for _, r := range runs {
		cur := latest[r.WorkItemID]
		if cur == nil || r.StartedAt.After(cur.StartedAt) || (r.StartedAt.Equal(cur.StartedAt) && r.ID > cur.ID) {
			latest[r.WorkItemID] = r
		}
	}
	streamLines := map[string]int{}
	for _, r := range latest {
		lines, rerr := readRunStream(s.Root, r.ID)
		if rerr != nil {
			streamLines[r.ID] = 0
			continue
		}
		streamLines[r.ID] = len(lines)
	}
	dead, inflight = next.ClassifyAttempts(s.now(), items, latest, streamLines)
	return dead, inflight, nil
}
