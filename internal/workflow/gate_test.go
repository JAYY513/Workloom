package workflow

import (
	"strings"
	"testing"
)

func gatePolicy(t *testing.T, extra string) *Policy {
	t.Helper()
	p, issues := testParse("workflows/sample.md", policyExtra(extra))
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatal("policy is nil")
	}
	return p
}

const gateFixture = `gates:
  exempt_stages:
    - draft
  stages:
    verification:
      require_artifacts:
        - test-results
      require_min_artifacts: 2
      require_comment: true
      require_approval: true
`

func TestCheckGateExemptAndAbsent(t *testing.T) {
	p := gatePolicy(t, gateFixture)
	if res := p.CheckGate("draft", GateEvidence{}); !res.Exempt || !res.Allowed {
		t.Errorf("draft = %+v", res)
	}
	if res := p.CheckGate("backlog", GateEvidence{}); res.Checked || !res.Allowed {
		t.Errorf("backlog = %+v", res)
	}
}

func TestCheckGateMissingEvidenceInOrder(t *testing.T) {
	p := gatePolicy(t, gateFixture)
	res := p.CheckGate("verification", GateEvidence{})
	if !res.Checked || res.Allowed {
		t.Fatalf("res = %+v", res)
	}
	want := []string{
		`artifact "test-results" is required`,
		"at least 2 artifacts are required (found 0)",
		"at least one comment event is required",
		"an approved, unconsumed approval is required",
	}
	if strings.Join(res.Missing, "\n") != strings.Join(want, "\n") {
		t.Fatalf("missing = %q", res.Missing)
	}
}

func TestCheckGateAllowsOnceEvidencePresent(t *testing.T) {
	p := gatePolicy(t, gateFixture)
	res := p.CheckGate("verification", GateEvidence{
		ArtifactNames: []string{"test-results", "plan"},
		CommentCount:  1,
		ApprovalReady: true,
	})
	if !res.Checked || !res.Allowed || len(res.Missing) != 0 {
		t.Fatalf("res = %+v", res)
	}

	// Duplicate artifact names count once.
	res = p.CheckGate("verification", GateEvidence{
		ArtifactNames: []string{"plan", "plan"},
		CommentCount:  1,
		ApprovalReady: true,
	})
	if res.Allowed || len(res.Missing) != 2 {
		t.Fatalf("duplicate names: %+v", res)
	}
}

func TestCheckGateApprovalFailsClosedWithoutEvidence(t *testing.T) {
	p := gatePolicy(t, "gates:\n  stages:\n    in_progress:\n      require_approval: true\n")
	res := p.CheckGate("in_progress", GateEvidence{ArtifactNames: []string{"x"}, CommentCount: 5})
	if res.Allowed || len(res.Missing) != 1 || !strings.Contains(res.Missing[0], "approval") {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckGateCarriesTheApprovalInvalidationNote(t *testing.T) {
	p := gatePolicy(t, "gates:\n  stages:\n    in_progress:\n      require_approval: true\n")
	note := "approval-2 was invalidated at 2026-09-20T05:14:03Z when the work item left \"ready\" (方案 §4.9)"
	res := p.CheckGate("in_progress", GateEvidence{ApprovalNote: note})
	if res.Allowed || len(res.Missing) != 1 || !strings.Contains(res.Missing[0], note) {
		t.Fatalf("res = %+v, want the note in the unmet requirement", res)
	}

	// With the approval in place the note is noise and stays out.
	res = p.CheckGate("in_progress", GateEvidence{ApprovalReady: true, ApprovalNote: note})
	if !res.Allowed || len(res.Missing) != 0 {
		t.Fatalf("res = %+v, want the gate to pass", res)
	}
}
