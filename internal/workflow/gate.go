package workflow

import (
	"fmt"
)

// GateEvidence is the evidence a stage gate consumes (方案 §4.7). The
// application layer collects it; approvals are wired by M3.7.
type GateEvidence struct {
	// ArtifactNames are the names of artifacts related to the work item.
	ArtifactNames []string
	// CommentCount is the number of comment events on the work item.
	CommentCount int
	// ApprovalReady reports that an approved, unconsumed approval exists for
	// the stage. Until M3.7 wires the lookup it stays false, so a gate that
	// requires approval fails closed (审批前不放行).
	ApprovalReady bool
}

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
	for _, want := range gate.RequireArtifacts {
		if !names[want] {
			res.Missing = append(res.Missing, fmt.Sprintf("artifact %q is required", want))
		}
	}
	if gate.RequireMinArtifacts > 0 && len(names) < gate.RequireMinArtifacts {
		res.Missing = append(res.Missing,
			fmt.Sprintf("at least %d artifacts are required (found %d)", gate.RequireMinArtifacts, len(names)))
	}
	if gate.RequireComment && ev.CommentCount < 1 {
		res.Missing = append(res.Missing, "at least one comment event is required")
	}
	if gate.RequireApproval && !ev.ApprovalReady {
		res.Missing = append(res.Missing, "an approved, unconsumed approval is required")
	}
	res.Allowed = len(res.Missing) == 0
	return res
}
