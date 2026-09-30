package workflow

import (
	"fmt"
)

// GateEvidence is the evidence a stage gate consumes (方案 §4.7). The
// application layer collects it; approvals are wired by M3.7.
type GateEvidence struct {
	// WorkItemID is the work item the gate is judging. Remediation text copies
	// it into commands; empty renders as <id>.
	WorkItemID string
	// ArtifactNames are the names of artifacts related to the work item.
	// Primary ownership (workitem_id) and association (related_workitems) both
	// qualify; the application layer decides which records match.
	ArtifactNames []string
	// CommentCount is the number of comment events on the work item.
	CommentCount int
	// ApprovalReady reports that an approved, unconsumed approval exists for
	// the stage. Until M3.7 wires the lookup it stays false, so a gate that
	// requires approval fails closed (审批前不放行).
	ApprovalReady bool
	// ApprovalNote explains a missing approval when the application layer can
	// (today: the previous approval was invalidated by 方案 §4.9). It is
	// appended to the unmet-requirement text and never changes the verdict.
	ApprovalNote string
}

// ArtifactRegisterUsage is the accepted parameter list for `artifact register`.
// Missing-artifact remediation copies it so the error stays a copyable contract
// with the CLI. --workitem sets primary ownership (workitem_id); --related adds
// association (related_workitems). Either satisfies the gate for that work item.
const ArtifactRegisterUsage = "artifact register --name N --actor <a> --reason <r> [--type document] [--path P] [--source S] [--run <run-id>] [--workitem <id>] [--workflow <id>] [--stage <stage>] [--status draft] [--related WLM-1]"

// GateResult is the outcome of checking one stage gate.
type GateResult struct {
	Stage string
	// Checked is false when the policy declares no gate for the stage.
	Checked bool
	// Exempt is true when the stage is listed in gates.exempt_stages.
	Exempt bool
	// Allowed is the overall outcome; when false, Missing lists every unmet
	// requirement in declaration order.
	Allowed bool
	Missing []string
}

// CheckGate evaluates the gate guarding entry into stage (方案 §4.7: a gate
// declares what must exist before a work item advances into that status).
// Stages listed in gates.exempt_stages are allowed without checks, as are
// stages without a declared gate. The check never fails open: every unmet
// requirement is reported.
func (p *Policy) CheckGate(stage string, ev GateEvidence) GateResult {
	res := GateResult{Stage: stage}
	if contains(p.Gates.ExemptStages, stage) {
		res.Exempt = true
		res.Allowed = true
		return res
	}
	gate, ok := p.Gates.Stages[stage]
	if !ok {
		res.Allowed = true
		return res
	}
	res.Checked = true

	names := map[string]bool{}
	for _, n := range ev.ArtifactNames {
		names[n] = true
	}
	id := gateWorkItemID(ev.WorkItemID)
	for _, want := range gate.RequireArtifacts {
		if !names[want] {
			res.Missing = append(res.Missing, fmt.Sprintf(
				"artifact %q is required for work item %s — primary ownership is --workitem (workitem_id); association is --related (related_workitems); either satisfies this gate. Accepted: %s. Register: `workloom artifact register --name %s --path <file> --workitem %s --actor <you> --reason <why>`",
				want, id, ArtifactRegisterUsage, want, id))
		}
	}
	if gate.RequireMinArtifacts > 0 && len(names) < gate.RequireMinArtifacts {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"at least %d artifacts are required (found %d) for work item %s — primary ownership is --workitem (workitem_id); association is --related (related_workitems); either satisfies this gate. Accepted: %s. Register: `workloom artifact register --name <name> --path <file> --workitem %s --actor <you> --reason <why>`",
			gate.RequireMinArtifacts, len(names), id, ArtifactRegisterUsage, id))
	}
	if gate.RequireComment && ev.CommentCount < 1 {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"at least one comment event is required — add one: `workloom workitem comment --id %s --text \"...\" --actor <you>`", id))
	}
	if gate.RequireApproval && !ev.ApprovalReady {
		msg := "an approved, unconsumed approval is required"
		if ev.ApprovalNote != "" {
			msg += " (" + ev.ApprovalNote + ")"
		}
		res.Missing = append(res.Missing, msg)
	}
	res.Allowed = len(res.Missing) == 0
	return res
}

func gateWorkItemID(id string) string {
	if id == "" {
		return "<id>"
	}
	return id
}
