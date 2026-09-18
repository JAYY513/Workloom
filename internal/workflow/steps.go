// Step selection for workflow instances (方案 §5.3, 实施计划 M3.6): the
// candidates out of the current step with their condition outcomes, in
// declaration order. Pure functions over a parsed Policy and Facts.
package workflow

// StepCandidate is one transition out of the current step.
type StepCandidate struct {
	To        string `json:"to"`
	When      string `json:"when,omitempty"`
	Satisfied bool   `json:"satisfied"`
}

// StepCandidates lists every declared transition out of step, in declaration
// order, with its condition evaluated against facts. Unconditional edges are
// always satisfied.
func (p *Policy) StepCandidates(step string, facts Facts) []StepCandidate {
	var out []StepCandidate
	for _, tr := range p.Transitions {
		if tr.From != step {
			continue
		}
		satisfied := true
		if tr.Cond != nil {
			satisfied = tr.Cond.Eval(facts)
		}
		out = append(out, StepCandidate{To: tr.To, When: tr.When, Satisfied: satisfied})
	}
	return out
}

// NextStep returns the first satisfied candidate in declaration order.
func (p *Policy) NextStep(step string, facts Facts) (StepCandidate, bool) {
	for _, cand := range p.StepCandidates(step, facts) {
		if cand.Satisfied {
			return cand, true
		}
	}
	return StepCandidate{}, false
}

// StepByID returns the declared step with the given id.
func (p *Policy) StepByID(id string) (Step, bool) {
	for _, st := range p.Steps {
		if st.ID == id {
			return st, true
		}
	}
	return Step{}, false
}
