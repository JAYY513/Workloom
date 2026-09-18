package workflow

import (
	"strings"
	"testing"
)

func mustCondition(t *testing.T, text string) *Condition {
	t.Helper()
	cond, err := parseCondition(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return cond
}

func TestParseConditionAcceptsWhitelistedForms(t *testing.T) {
	for _, text := range []string{
		"workitem.clarification_needed == false",
		"workitem.approval_required != true",
		"workitem.priority > 3",
		"workitem.priority < 10",
		"workitem.status in ready,review",
		"workitem.status in ready, review",
		"workitem.parent_id exists",
		"workitem.type == feature",
		"workitem.status == 'review'",
		`workitem.status == "review"`,
		"workitem.status in 'ready', \"review\"",
		"workitem.parent_id == ''",
	} {
		if _, err := parseCondition(text); err != nil {
			t.Errorf("parse %q: %v", text, err)
		}
	}
}

func TestParseConditionRejectsPolicyErrors(t *testing.T) {
	cases := []struct{ text, want string }{
		{"workitem.missing == 1", "unknown field"},
		{"workitem.priority ~~ 3", "unknown operator"},
		{"workitem.status && workitem.type", "compound"},
		{"workitem.clarification_needed == 3", "bool"},
		{"workitem.priority == three", "int"},
		{"workitem.priority in 1,2", "string"},
		{"workitem.clarification_needed exists", "exists is only supported"},
		{"workitem.priority >", "malformed"},
		{"workitem.status in ready,,review", "empty literal"},
		{"workitem.status in ready,ready", "duplicate literal"},
		{`workitem.status == "review`, "unbalanced quote"},
		{"workitem.status in ready,\"review", "unbalanced quote"},
		{"", "empty condition"},
	}
	for _, tc := range cases {
		_, err := parseCondition(tc.text)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parse %q: err = %v, want %q", tc.text, err, tc.want)
		}
	}
}

func TestConditionEval(t *testing.T) {
	facts := Facts{
		ID: "WLM-1", Status: "ready", Type: "task", Priority: 5,
		ClarificationNeeded: false, ApprovalRequired: true, ParentID: "WLM-0",
	}
	cases := []struct {
		text string
		want bool
	}{
		{"workitem.id == WLM-1", true},
		{"workitem.id != WLM-1", false},
		{"workitem.clarification_needed == false", true},
		{"workitem.clarification_needed == true", false},
		{"workitem.approval_required == true", true},
		{"workitem.priority > 3", true},
		{"workitem.priority < 3", false},
		{"workitem.priority == 5", true},
		{"workitem.status in ready,review", true},
		{"workitem.status in backlog,review", false},
		{"workitem.status in ready, review", true},
		{"workitem.status == 'ready'", true},
		{`workitem.status == "ready"`, true},
		{"workitem.parent_id exists", true},
		{"workitem.parent_id == WLM-0", true},
		{"workitem.type == feature", false},
	}
	for _, tc := range cases {
		if got := mustCondition(t, tc.text).Eval(facts); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.text, got, tc.want)
		}
	}
	if mustCondition(t, "workitem.parent_id exists").Eval(Facts{}) {
		t.Error("parent_id exists should be false without a parent")
	}
	if !mustCondition(t, "workitem.parent_id == ''").Eval(Facts{}) {
		t.Error("quoted empty literal should match a missing parent")
	}
}

const branchPolicy = `---
id: sample
name: 分支示例
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
  - id: clarify
    type: clarify
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: inspect
    to: clarify
    when: workitem.clarification_needed == true
  - from: inspect
    to: done
---
`

func TestStepCandidatesDeclarationOrderAndNext(t *testing.T) {
	p, issues := testParse("workflows/sample.md", branchPolicy)
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatal("policy is nil")
	}

	cands := p.StepCandidates("inspect", Facts{ClarificationNeeded: false})
	if len(cands) != 3 {
		t.Fatalf("candidates = %+v", cands)
	}
	if cands[0].To != "implement" || !cands[0].Satisfied {
		t.Errorf("candidates[0] = %+v", cands[0])
	}
	if cands[1].To != "clarify" || cands[1].Satisfied {
		t.Errorf("candidates[1] = %+v", cands[1])
	}
	if cands[2].To != "done" || !cands[2].Satisfied {
		t.Errorf("candidates[2] (unconditional) = %+v", cands[2])
	}

	if next, ok := p.NextStep("inspect", Facts{ClarificationNeeded: true}); !ok || next.To != "clarify" {
		t.Errorf("next = %+v/%v", next, ok)
	}
	if next, ok := p.NextStep("inspect", Facts{ClarificationNeeded: false}); !ok || next.To != "implement" {
		t.Errorf("next = %+v/%v", next, ok)
	}
}

func TestParseRejectsUnknownConditionField(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
transitions:
  - from: inspect
    to: implement
    when: workitem.verification_failed == true
---
`
	p, issues := testParse("workflows/sample.md", src)
	if p != nil {
		t.Fatalf("policy = %+v, want nil", p)
	}
	is := findIssue(t, issues, SeverityError, "transitions[0].when")
	if is.Line == 0 || !strings.Contains(is.Reason, "unknown field") {
		t.Errorf("issue = %s", is.String())
	}
}
