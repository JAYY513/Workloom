package workflow

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderSubstitutesVariables(t *testing.T) {
	body := "task {{workitem.id}} for {{ name }}.\nagain {{workitem.id}}\n"
	got, err := Render(body, map[string]string{"workitem.id": "WLM-1", "name": "张三"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "task WLM-1 for 张三.\nagain WLM-1\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderUnknownVariablesFail(t *testing.T) {
	got, err := Render("a {{missing}} b {{also_missing}} c {{missing}}", map[string]string{"other": "x"})
	var re *RenderError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v", err)
	}
	if re.Kind != RenderUnknownVariables {
		t.Errorf("kind = %q", re.Kind)
	}
	if strings.Join(re.Variables, ",") != "also_missing,missing" {
		t.Errorf("variables = %v", re.Variables)
	}
	// The failed render never leaks a partial substitution result.
	if got != "" {
		t.Errorf("partial output returned: %q", got)
	}
}

func TestRenderSyntaxErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		line int
	}{
		{"unclosed", "line one\nline two {{broken", 2},
		{"empty name", "{{}}", 1},
		{"leading digit", "{{9x}}", 1},
		{"space in name", "{{a b}}", 1},
		{"stray close", "a }} b", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render(tc.body, nil)
			var re *RenderError
			if !errors.As(err, &re) {
				t.Fatalf("err = %v", err)
			}
			if re.Kind != RenderSyntax || re.Line != tc.line {
				t.Errorf("kind=%q line=%d (%v)", re.Kind, re.Line, err)
			}
		})
	}
}

func TestPolicyRenderUsesBody(t *testing.T) {
	p, issues := testParse("workflows/sample.md", policyExtra(""))
	requireNoErrors(t, issues)
	if p == nil {
		t.Fatal("policy is nil")
	}
	p.Body = "hello {{who}}"
	got, err := p.Render(map[string]string{"who": "world"})
	if err != nil || got != "hello world" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestParseRejectsBadTemplateWithAbsoluteLine(t *testing.T) {
	src := "---\nid: sample\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\nline one\nline two {{broken\n"
	p, issues := testParse("workflows/sample.md", src)
	if p != nil {
		t.Fatalf("policy = %+v, want nil", p)
	}
	is := findIssue(t, issues, SeverityError, "body")
	if is.Line != 10 {
		t.Errorf("line = %d, want 10 (%s)", is.Line, is.String())
	}
	if !strings.Contains(is.Reason, "unclosed {{") {
		t.Errorf("reason = %q", is.Reason)
	}
}

func TestParseKeepsEnvReferencesRaw(t *testing.T) {
	src := "---\nid: sample\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\nrun with $SECRET_TOKEN and ${OTHER}\n"
	p, issues := testParse("workflows/sample.md", src)
	requireNoErrors(t, issues)
	if p == nil || !strings.Contains(p.Body, "$SECRET_TOKEN") || !strings.Contains(p.Body, "${OTHER}") {
		t.Fatalf("policy = %+v", p)
	}
}

func TestExpandEnv(t *testing.T) {
	env := map[string]string{"TOKEN": "s3cret", "DIR": "/tmp/x"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	got, err := ExpandEnv("a $TOKEN b ${DIR} c $$ d $5", lookup)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got != "a s3cret b /tmp/x c $ d $5" {
		t.Fatalf("got %q", got)
	}

	_, err = ExpandEnv("$MISSING and ${ALSO} and $MISSING", lookup)
	var re *RenderError
	if !errors.As(err, &re) || re.Kind != RenderEnvUndefined {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(re.Variables, ",") != "ALSO,MISSING" {
		t.Errorf("variables = %v", re.Variables)
	}

	for _, bad := range []string{"${unclosed", "${}"} {
		_, err = ExpandEnv(bad, lookup)
		if !errors.As(err, &re) || re.Kind != RenderSyntax {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}
