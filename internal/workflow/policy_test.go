package workflow

import (
	"strings"
	"testing"
)

// minimalPolicy returns a valid policy whose id matches a file named <id>.md.
func minimalPolicy(id string) string {
	return "---\nid: " + id + "\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n"
}

// policyExtra returns the standard policy skeleton (id sample) with extra
// front matter lines inserted before the closing delimiter.
func policyExtra(extra string) string {
	return "---\nid: sample\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n" + extra + "---\n"
}

const validPolicy = `---
id: sample
name: 示例工作流
version: 2
input:
  required:
    - workitem
  optional:
    - project_context
steps:
  - id: inspect
    type: inspect
    required: true
  - id: implement
    type: execute
    required: false
    status: in_progress
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: implement
    to: done
approval_points:
  - architecture_change
completion_rules:
  - acceptance_criteria_verified
gates:
  exempt_stages:
    - draft
  stages:
    review:
      require_artifacts:
        - plan
      require_min_artifacts: 2
      require_comment: true
      require_approval: true
hooks:
  before_run:
    command: git fetch
    timeout_seconds: 30
concurrency:
  global: 4
  per_status:
    in_progress: 2
limits:
  max_attempts: 3
  run_timeout_seconds: 600
  stall_threshold_seconds: 300
  backoff_max_seconds: 900
quality_gate:
  min_score: 55
on_reject: regress:ready
---
prompt body
line two
`

func testParse(rel, src string) (*Policy, []Issue) {
	return Parse(rel, []byte(src))
}

func errorIssues(issues []Issue) []Issue {
	var out []Issue
	for _, is := range issues {
		if is.Severity == SeverityError {
			out = append(out, is)
		}
	}
	return out
}

func warningIssues(issues []Issue) []Issue {
	var out []Issue
	for _, is := range issues {
		if is.Severity == SeverityWarning {
			out = append(out, is)
		}
	}
	return out
}

func requireNoErrors(t *testing.T, issues []Issue) {
	t.Helper()
	for _, is := range errorIssues(issues) {
		t.Fatalf("unexpected error issue: %s", is.String())
	}
}

func findIssue(t *testing.T, issues []Issue, sev Severity, field string) Issue {
	t.Helper()
	for _, is := range issues {
		if is.Severity == sev && is.Field == field {
			return is
		}
	}
	t.Fatalf("no %s issue for field %q in %v", sev, field, issues)
	return Issue{}
}

func TestParseValidPolicy(t *testing.T) {
	p, issues := testParse("workflows/sample.md", validPolicy)
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatalf("policy is nil; issues=%v", issues)
	}
	if p.ID != "sample" || p.Name != "示例工作流" || p.Version != 2 {
		t.Errorf("identity = %q/%q/%d", p.ID, p.Name, p.Version)
	}
	if p.File != "workflows/sample.md" {
		t.Errorf("file = %q", p.File)
	}
	if len(p.Input.Required) != 1 || p.Input.Required[0] != "workitem" || len(p.Input.Optional) != 1 {
		t.Errorf("input = %+v", p.Input)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("steps = %+v", p.Steps)
	}
	if s := p.Steps[0]; s.ID != "inspect" || s.Type != "inspect" || !s.Required || s.Status != "" {
		t.Errorf("steps[0] = %+v", s)
	}
	if s := p.Steps[1]; s.ID != "implement" || s.Required || s.Status != "in_progress" {
		t.Errorf("steps[1] = %+v", s)
	}
	if len(p.Transitions) != 2 {
		t.Fatalf("transitions = %+v", p.Transitions)
	}
	if tr := p.Transitions[0]; tr.From != "inspect" || tr.To != "implement" || tr.When == "" {
		t.Errorf("transitions[0] = %+v", tr)
	}
	if tr := p.Transitions[1]; tr.When != "" || tr.To != "done" {
		t.Errorf("transitions[1] = %+v", tr)
	}
	gate := p.Gates.Stages["review"]
	if len(gate.RequireArtifacts) != 1 || gate.RequireMinArtifacts != 2 || !gate.RequireComment || !gate.RequireApproval {
		t.Errorf("gate = %+v", gate)
	}
	if len(p.Gates.ExemptStages) != 1 || p.Gates.ExemptStages[0] != "draft" {
		t.Errorf("exempt = %v", p.Gates.ExemptStages)
	}
	if h := p.Hooks["before_run"]; h.Command != "git fetch" || h.TimeoutSeconds != 30 {
		t.Errorf("hook = %+v", h)
	}
	if p.Concurrency.Global != 4 || p.Concurrency.PerStatus["in_progress"] != 2 {
		t.Errorf("concurrency = %+v", p.Concurrency)
	}
	if p.Limits.MaxAttempts != 3 || p.Limits.RunTimeoutSeconds != 600 || p.Limits.StallThresholdSeconds != 300 || p.Limits.BackoffMaxSeconds != 900 {
		t.Errorf("limits = %+v", p.Limits)
	}
	if p.QualityGate.MinScore != 55 {
		t.Errorf("quality_gate = %+v", p.QualityGate)
	}
	if p.OnReject != "regress:ready" {
		t.Errorf("on_reject = %q", p.OnReject)
	}
	if p.Body != "prompt body\nline two\n" {
		t.Errorf("body = %q", p.Body)
	}
}

func TestParseDefaultsOnRejectBlock(t *testing.T) {
	p, issues := testParse("workflows/sample.md", policyExtra(""))
	requireNoErrors(t, issues)
	if p == nil || p.OnReject != "block" {
		t.Fatalf("policy = %+v", p)
	}
}

func TestParseFrontMatterErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"missing opening", "id: sample\n---\nbody", "missing front matter"},
		{"missing closing", "---\nid: sample\nbody\n", "missing closing ---"},
		{"empty front matter", "---\n---\nbody", "empty front matter"},
		{"second document", "---\nid: sample\n--- # not the close\nmore: 1\n---\nbody", "second document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, issues := testParse("workflows/sample.md", tc.src)
			if p != nil {
				t.Fatalf("policy = %+v, want nil", p)
			}
			errs := errorIssues(issues)
			if len(errs) == 0 {
				t.Fatalf("no error issues: %v", issues)
			}
			if !strings.Contains(errs[0].Reason, tc.want) {
				t.Fatalf("reason %q does not contain %q", errs[0].Reason, tc.want)
			}
		})
	}
}

func TestParseUnknownKeysAreWarnings(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
future_key: true
steps:
  - id: inspect
    type: inspect
    extra: 1
---
`
	p, issues := testParse("workflows/sample.md", src)
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatalf("policy is nil; issues=%v", issues)
	}
	warns := warningIssues(issues)
	if len(warns) != 2 {
		t.Fatalf("warnings = %v", warns)
	}
	if warns[0].Field != "future_key" || warns[0].Line != 5 {
		t.Errorf("warnings[0] = %s", warns[0].String())
	}
	if warns[1].Field != "steps[0].extra" || warns[1].Line != 9 {
		t.Errorf("warnings[1] = %s", warns[1].String())
	}
}

func TestParseDuplicateKeysFail(t *testing.T) {
	src := `---
id: sample
name: 示例
name: 另一个
version: 1
steps:
  - id: inspect
    type: inspect
---
`
	p, issues := testParse("workflows/sample.md", src)
	if p != nil {
		t.Fatalf("policy = %+v, want nil", p)
	}
	is := findIssue(t, issues, SeverityError, "name")
	if !strings.Contains(is.Reason, "duplicate key") {
		t.Errorf("reason = %q", is.Reason)
	}
}

func TestParseMissingRequired(t *testing.T) {
	_, issues := testParse("workflows/sample.md", "---\n{}\n---\n")
	fields := map[string]bool{}
	for _, is := range errorIssues(issues) {
		fields[is.Field] = true
	}
	for _, want := range []string{"id", "name", "version", "steps"} {
		if !fields[want] {
			t.Errorf("no required error for %q: %v", want, issues)
		}
	}
}

func TestParseTypeErrors(t *testing.T) {
	src := `---
id: sample
name: 示例
version: "one"
steps:
  - id: inspect
    type: inspect
    required: "yes"
gates:
  stages:
    review:
      require_comment: 1
---
`
	_, issues := testParse("workflows/sample.md", src)
	for _, want := range []string{"version", "steps[0].required", "gates.stages.review.require_comment"} {
		findIssue(t, issues, SeverityError, want)
	}
}

func TestParseIDMismatchAndShape(t *testing.T) {
	_, issues := testParse("workflows/sample.md", minimalPolicy("other"))
	is := findIssue(t, issues, SeverityError, "id")
	if !strings.Contains(is.Reason, "does not match") {
		t.Errorf("reason = %q", is.Reason)
	}

	_, issues = testParse("workflows/sample.md", minimalPolicy("Sample"))
	is = findIssue(t, issues, SeverityError, "id")
	if !strings.Contains(is.Reason, "invalid id") {
		t.Errorf("reason = %q", is.Reason)
	}
}

func TestParseStepErrors(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
  - id: inspect
    type: inspect
  - id: 9bad
    type: inspect
  - id: check
    type: inspect
    status: wip
---
`
	_, issues := testParse("workflows/sample.md", src)
	findIssue(t, issues, SeverityError, "steps[1].id")
	findIssue(t, issues, SeverityError, "steps[2].id")
	findIssue(t, issues, SeverityError, "steps[3].status")
}

func TestParseTransitionErrors(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
transitions:
  - from: ghost
    to: inspect
  - from: inspect
    to: ghost
  - from: inspect
    to: done
  - from: inspect
    to: done
---
`
	_, issues := testParse("workflows/sample.md", src)
	findIssue(t, issues, SeverityError, "transitions[0].from")
	findIssue(t, issues, SeverityError, "transitions[1].to")
	findIssue(t, issues, SeverityError, "transitions[3]")
}

func TestParseGateErrors(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
gates:
  exempt_stages:
    - wip
  stages:
    nope:
      require_comment: true
    review:
      require_min_artifacts: -1
      require_artifacts:
        - Bad-Name
---
`
	_, issues := testParse("workflows/sample.md", src)
	findIssue(t, issues, SeverityError, "gates.exempt_stages[0]")
	findIssue(t, issues, SeverityError, "gates.stages.nope")
	findIssue(t, issues, SeverityError, "gates.stages.review.require_min_artifacts")
	findIssue(t, issues, SeverityError, "gates.stages.review.require_artifacts[0]")
}

func TestParseHookIssues(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
hooks:
  made_up:
    command: echo
  before_run:
    timeout_seconds: 0
---
`
	_, issues := testParse("workflows/sample.md", src)
	findIssue(t, issues, SeverityError, "hooks.made_up")
	findIssue(t, issues, SeverityError, "hooks.before_run.command")
	findIssue(t, issues, SeverityError, "hooks.before_run.timeout_seconds")
}

func TestParseLimitsConcurrencyAndQualityGate(t *testing.T) {
	src := policyExtra(`concurrency:
  global: 0
  per_status:
    wip: 1
limits:
  max_attempts: 0
quality_gate:
  min_score: 101
on_reject: stop
`)
	_, issues := testParse("workflows/sample.md", src)
	findIssue(t, issues, SeverityError, "concurrency.global")
	findIssue(t, issues, SeverityError, "concurrency.per_status.wip")
	findIssue(t, issues, SeverityError, "limits.max_attempts")
	findIssue(t, issues, SeverityError, "quality_gate.min_score")
	findIssue(t, issues, SeverityError, "on_reject")

	_, issues = testParse("workflows/sample.md", policyExtra("on_reject: regress:wip\n"))
	findIssue(t, issues, SeverityError, "on_reject")

	// quality_gate present without min_score is an error, not a default.
	_, issues = testParse("workflows/sample.md", policyExtra("quality_gate: {}\n"))
	findIssue(t, issues, SeverityError, "quality_gate.min_score")
}

func TestParseCollectsAllErrors(t *testing.T) {
	src := policyExtra(`version_ignored: true
quality_gate:
  min_score: 200
on_reject: nope
`)
	src = strings.Replace(src, "version: 1", "version: 0", 1)
	_, issues := testParse("workflows/sample.md", src)
	errs := errorIssues(issues)
	if len(errs) < 3 {
		t.Fatalf("errors = %v", errs)
	}
	fields := map[string]bool{}
	for _, is := range errs {
		fields[is.Field] = true
	}
	for _, want := range []string{"version", "quality_gate.min_score", "on_reject"} {
		if !fields[want] {
			t.Errorf("no error for %q: %v", want, errs)
		}
	}
}

func TestParseLineNumbers(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
  - id: inspect
    type: inspect
---
`
	_, issues := testParse("workflows/sample.md", src)
	is := findIssue(t, issues, SeverityError, "steps[1].id")
	if is.Line != 8 {
		t.Errorf("line = %d, want 8 (%s)", is.Line, is.String())
	}
}

func TestParseCRLFFrontMatter(t *testing.T) {
	src := "---\r\nid: sample\r\nname: 示例\r\nversion: 1\r\nsteps:\r\n  - id: inspect\r\n    type: inspect\r\n---\r\nbody\r\n"
	p, issues := testParse("workflows/sample.md", src)
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatalf("policy is nil; issues=%v", issues)
	}
	if p.Body != "body\r\n" {
		t.Errorf("body = %q", p.Body)
	}
}

func TestParseBOMFrontMatter(t *testing.T) {
	src := "\xef\xbb\xbf---\nid: sample\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\nbody\n"
	p, issues := testParse("workflows/sample.md", src)
	requireNoErrors(t, issues)
	if p == nil || p.ID != "sample" {
		t.Fatalf("policy = %+v issues=%v", p, issues)
	}
}

// The body starts right after the closing delimiter's line break: a blank
// line following the delimiter is body content, not part of the framing.
func TestParseCRLFBlankLineAfterDelimiter(t *testing.T) {
	src := "---\r\nid: sample\r\nname: 示例\r\nversion: 1\r\nsteps:\r\n  - id: inspect\r\n    type: inspect\r\n---\r\n\r\nbody\r\n"
	p, issues := testParse("workflows/sample.md", src)
	requireNoErrors(t, issues)
	if p == nil || p.Body != "\r\nbody\r\n" {
		t.Fatalf("body = %q", p.Body)
	}
}

func TestParseEmptyStepsFails(t *testing.T) {
	src := `---
id: sample
name: 示例
version: 1
steps: []
---
`
	_, issues := testParse("workflows/sample.md", src)
	is := findIssue(t, issues, SeverityError, "steps")
	if !strings.Contains(is.Reason, "must not be empty") {
		t.Errorf("reason = %q", is.Reason)
	}
}
