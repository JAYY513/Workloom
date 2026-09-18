package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/next"
	"workloom/internal/prompt"
	"workloom/internal/storage"
	"workloom/internal/workflow"
)

// promptVarNames are the template variables a workflow body may reference
// (M3.2 renders strictly: an unknown name is an error, never an empty string).
const promptVarNames = "project.id, project.name, run.id, workitem.id, workitem.title, workitem.type, workitem.status, step"

// RunPromptRequest assembles one round's prompt for a run.
type RunPromptRequest struct {
	RunID string
	// Round selects the round; 0 assembles the next one.
	Round int
	// Write materializes the text under .devsys/local/runs/<run-id>/ (not
	// committed, regenerable) and reports the path.
	Write bool
}

// RunPromptView is one assembled round: the text the harness receives, its
// hash (the only proof of what was handed over) and the pointers it refers to.
type RunPromptView struct {
	RunID   string   `json:"run_id"`
	Round   int      `json:"round"`
	Mode    string   `json:"mode"`
	Hash    string   `json:"hash"`
	Path    string   `json:"path,omitempty"`
	Refs    []string `json:"refs"`
	Text    string   `json:"text"`
	Notices []string `json:"notices,omitempty"`
	// Snapshot is what this round's context was assembled from; it is recorded
	// with the round so the attempt's evidence says what the agent saw (方案 §11.3).
	Snapshot prompt.Snapshot `json:"snapshot"`
}

// RunPrompt assembles a round's prompt from the run's own facts: the first
// round carries the task brief, the rendered policy body, the readiness state,
// the context pointers and the reporting protocol; later rounds carry only what
// changed and what is left (方案 §4.8). Assembly is deterministic, so the same
// round can be replayed and compared by hash.
func (s *Service) RunPrompt(ctx context.Context, req RunPromptRequest) (RunPromptView, error) {
	if req.RunID == "" {
		return RunPromptView{}, Usagef("run prompt requires a run id")
	}
	md, err := s.project()
	if err != nil {
		return RunPromptView{}, err
	}
	r, _, err := readRun(ctx, s, req.RunID)
	if err != nil {
		return RunPromptView{}, err
	}
	wi, err := s.WorkitemGet(ctx, r.WorkItemID)
	if err != nil {
		return RunPromptView{}, err
	}
	lines, err := readRunStream(s.Root, r.ID)
	if err != nil {
		return RunPromptView{}, Internalf("read run stream: %v", err)
	}
	round := req.Round
	if round <= 0 {
		round = nextRound(lines)
	}

	var notices []string
	res, err := s.policyForWorkItem(ctx, wi.Item)
	if err != nil {
		return RunPromptView{}, s.errWorkflowPolicy(err)
	}
	policyBrief := prompt.Policy{}
	if res.Policy != nil {
		if res.Issue != nil {
			// A broken policy blocks new work (the claim path does the same);
			// a continuation may still proceed on the last-known-good copy.
			if round == 1 {
				return RunPromptView{}, s.errWorkflowPolicy(fmt.Errorf(
					"workflow policy %q is invalid: %s; prompts stay blocked until the file is fixed (%s is retained for read paths)",
					res.ID, res.Issue.String(), res.Source))
			}
			notices = append(notices, fmt.Sprintf("workflow policy %q is invalid: %s; the continuation uses %s", res.ID, res.Issue.String(), res.Source))
		}
		policyBrief = prompt.Policy{ID: res.ID, Body: res.Policy.Body, Vars: promptVars(md.Project, r, wi.Item)}
	}

	report, _, err := s.Next(ctx)
	if err != nil {
		return RunPromptView{}, err
	}
	spec, err := s.specText(ctx, wi.Item.ID)
	if err != nil {
		return RunPromptView{}, err
	}
	wctx, err := s.ContextForWorkitem(ctx, wi.Item.ID, nil)
	if err != nil {
		return RunPromptView{}, err
	}

	in := prompt.Input{
		Round: round,
		Task: prompt.Task{
			ID: wi.Item.ID, Title: wi.Item.Title, Type: wi.Item.Type, Status: wi.Item.Status,
			Description: wi.Item.Description, Spec: spec,
			AcceptanceCriteria: wi.Item.AcceptanceCriteria, Constraints: wi.Item.Constraints,
			Dependencies: wi.Item.Dependencies,
			WorkflowID:   workflowID(wi.Item), WorkflowStep: workflowStep(wi.Item), WorkflowPaused: workflowPaused(wi.Item),
		},
		Policy: policyBrief,
		State:  prompt.State{Verdict: report.Verdict, Next: report.Next.Action, Risks: riskLines(report.Risks)},
		Context: prompt.Context{
			Decisions: recordPromptRefs(wctx.Decisions), Findings: recordPromptRefs(wctx.Findings),
			Artifacts: recordPromptRefs(wctx.Artifacts), Comments: eventPromptRefs(wctx.Comments),
			Knowledge: knowledgePromptRefs(wctx.Knowledge.Pages),
			Notes:     knowledgeNotes(wctx.Knowledge),
		},
		Snapshot: prompt.Snapshot{
			ProjectStateVersion: projectStateVersion(md),
			WorkitemVersion:     wi.Version,
			ArtifactVersions:    artifactVersions(wctx.Artifacts),
			KnowledgeRevision:   wctx.Knowledge.Baseline,
			KnowledgePages:      knowledgePagePaths(wctx.Knowledge.Pages),
			DecisionIDs:         decisionIDs(wctx.Decisions),
			WorkspaceHead:       r.Claim.HeadSHA,
			KnowledgeBehind:     knowledgeBehind(r.Claim.HeadSHA, wctx.Knowledge.Baseline),
			KnowledgeDegraded:   wctx.Knowledge.Degraded,
		},
	}
	if round > 1 {
		boundary := time.Time{}
		if last, ok := lastRound(lines); ok {
			boundary = last.Time
		}
		changes, err := s.promptChanges(ctx, boundary)
		if err != nil {
			return RunPromptView{}, err
		}
		in.Changes = changes
		in.Remaining = promptRemaining(res.Policy, wi.Item)
	}

	assembled, err := prompt.Assemble(in)
	if err != nil {
		return RunPromptView{}, Invalidf(KindWorkflow, nil, "%v", err)
	}
	view := RunPromptView{
		RunID: r.ID, Round: assembled.Round, Mode: string(assembled.Mode),
		Hash: assembled.Hash, Refs: assembled.Refs, Text: assembled.Text, Notices: notices,
		Snapshot: assembled.Snapshot,
	}
	if req.Write {
		path, err := writePromptFile(s.Root, r.ID, assembled)
		if err != nil {
			return RunPromptView{}, Internalf("write prompt file: %v", err)
		}
		view.Path = path
	}
	return view, nil
}

// promptVars fills the template variables a policy body may reference.
func promptVars(project *domain.Project, r *domain.Run, wi *domain.WorkItem) map[string]string {
	vars := map[string]string{
		"run.id":          r.ID,
		"workitem.id":     wi.ID,
		"workitem.title":  wi.Title,
		"workitem.type":   wi.Type,
		"workitem.status": wi.Status,
		"step":            workflowStep(wi),
	}
	if project != nil {
		vars["project.id"] = project.ID
		vars["project.name"] = project.Name
	}
	return vars
}

func workflowID(wi *domain.WorkItem) string {
	if wi.Workflow == nil {
		return ""
	}
	return wi.Workflow.ID
}

func workflowStep(wi *domain.WorkItem) string {
	if wi.Workflow == nil {
		return ""
	}
	return wi.Workflow.Step
}

func workflowPaused(wi *domain.WorkItem) bool {
	return wi.Workflow != nil && wi.Workflow.Paused
}

// specText reads the work item's specification body (.devsys/specs/<id>.md) —
// optional: most work items carry their statement in the description.
func (s *Service) specText(ctx context.Context, workItemID string) (string, error) {
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return "", s.storeError(err)
	}
	var body string
	err = st.Read(ctx, func(rd *storage.Reader) error {
		data, exists, err := rd.Read("specs/" + workItemID + ".md")
		if err != nil {
			return err
		}
		if exists {
			body = string(data)
		}
		return nil
	})
	if err != nil {
		return "", s.storeError(err)
	}
	return body, nil
}

// promptChanges lists what happened since the previous round started. An empty
// boundary (no earlier round) yields nothing: a full prompt needs no delta.
func (s *Service) promptChanges(ctx context.Context, since time.Time) ([]prompt.Change, error) {
	if since.IsZero() {
		return nil, nil
	}
	evs, err := s.EventList(ctx, EventListRequest{Since: since, Limit: 20})
	if err != nil {
		return nil, err
	}
	changes := make([]prompt.Change, 0, len(evs))
	for _, ev := range evs {
		changes = append(changes, prompt.Change{
			Kind:   ev.Type,
			Ref:    "event://" + ev.ID,
			Detail: firstLine(ev.Content),
		})
	}
	return changes, nil
}

// promptRemaining lists what the round still has to cover: the workflow steps
// after the current one, plus anything that visibly holds the work item back.
func promptRemaining(policy *workflow.Policy, wi *domain.WorkItem) []string {
	var out []string
	if policy != nil && len(policy.Steps) > 0 {
		current := workflowStep(wi)
		seen := false
		for _, step := range policy.Steps {
			if step.ID == current {
				seen = true
				out = append(out, "当前步骤："+step.ID)
				continue
			}
			if seen {
				out = append(out, "待完成步骤："+step.ID)
			}
		}
		if !seen && current != "" {
			out = append(out, "当前步骤："+current+"（不在策略步骤表中）")
		}
	}
	if wi.Status == domain.StatusBlocked {
		out = append(out, "工作项处于 blocked，需先解除阻塞")
	}
	if wi.ClarificationNeeded {
		out = append(out, "clarification_needed 为真，需先澄清")
	}
	if wi.ApprovalRequired {
		out = append(out, "工作项声明需要审批，推进前先走 approval 流程")
	}
	return out
}

func recordPromptRefs(refs []RecordRef) []prompt.Ref {
	out := make([]prompt.Ref, 0, len(refs))
	for _, ref := range refs {
		out = append(out, prompt.Ref{Ref: ref.Ref, Title: ref.Title, Extra: ref.Status})
	}
	return out
}

func eventPromptRefs(refs []EventRef) []prompt.Ref {
	out := make([]prompt.Ref, 0, len(refs))
	for _, ref := range refs {
		out = append(out, prompt.Ref{Ref: "event://" + ref.ID, Title: ref.Type, Extra: ref.Actor})
	}
	return out
}

func riskLines(risks []next.Risk) []string {
	out := make([]string, 0, len(risks))
	for _, risk := range risks {
		out = append(out, risk.Kind+": "+risk.Detail)
	}
	return out
}

// writePromptFile materializes the assembled text under .devsys/local/ (not
// committed, regenerable) and returns its project-relative path.
func writePromptFile(root, runID string, p prompt.Prompt) (string, error) {
	rel := filepath.Join(".devsys", "local", "runs", runID, fmt.Sprintf("round-%d.md", p.Round))
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(p.Text), 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func firstLine(text string) string {
	line := text
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	return line
}

// knowledgePromptRefs turns the selected pages into prompt references: a path
// the agent can read, with the page's own description as the title.
func knowledgePromptRefs(pages []KnowledgePageRef) []prompt.Ref {
	refs := make([]prompt.Ref, 0, len(pages))
	for _, page := range pages {
		extra := page.Status
		if page.Stale {
			extra += " stale"
		}
		refs = append(refs, prompt.Ref{Ref: page.Path, Title: page.Description, Extra: strings.TrimSpace(extra)})
	}
	return refs
}

// knowledgeNotes carries the knowledge layer's own caveat into the round: a
// layer that cannot date its pages is a fact the agent should not have to
// rediscover.
func knowledgeNotes(context KnowledgeContext) []string {
	if context.Notice == "" {
		return nil
	}
	return []string{context.Notice}
}

// knowledgePagePaths lists the pages a round's knowledge context used.
func knowledgePagePaths(pages []KnowledgePageRef) []string {
	paths := make([]string, 0, len(pages))
	for _, page := range pages {
		paths = append(paths, page.Path)
	}
	return paths
}

// artifactVersions records which artifacts the round's context carried. The
// context keeps references, not versions, so the reference itself is what the
// snapshot can promise.
func artifactVersions(refs []RecordRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Ref)
	}
	return out
}

// decisionIDs lists the decisions a round's context carried.
func decisionIDs(refs []RecordRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Ref)
	}
	return out
}

// projectStateVersion stands in for the project state's version counter: the
// managed state records no counter, and its last update is the honest answer to
// "which state did this round read" (方案 §11.3).
func projectStateVersion(md *config.Metadata) string {
	if md == nil || md.Project == nil {
		return ""
	}
	return md.Project.UpdatedAt.UTC().Format(time.RFC3339)
}

// knowledgeBehind reports whether the workspace ran ahead of the knowledge
// baseline. Knowledge is not split per worktree (方案 §11.3): a workspace ahead
// is annotated, not re-indexed.
func knowledgeBehind(workspaceHead, knowledgeRevision string) bool {
	return workspaceHead != "" && knowledgeRevision != "" && workspaceHead != knowledgeRevision
}
