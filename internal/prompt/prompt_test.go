package prompt

import (
	"strings"
	"testing"
)

func fullInput() Input {
	return Input{
		Round: 1,
		Task: Task{
			ID: "WLM-7", Title: "实现登录限流", Type: "task", Status: "in_progress",
			Description:        "给登录接口加限流。",
			Spec:               "每秒最多 5 次。",
			AcceptanceCriteria: []string{"超限返回 429", "有回归测试"},
			Constraints:        []string{"不改公共 API"},
			Dependencies:       []string{"WLM-3"},
			WorkflowID:         "feature-development", WorkflowStep: "implement",
		},
		Policy: Policy{
			ID:   "feature-development",
			Body: "按 {{step}} 步骤实现，目标：{{title}}。",
			Vars: map[string]string{"step": "implement", "title": "实现登录限流"},
		},
		State: State{Verdict: "CONCERNS", Next: "start WLM-7", Risks: []string{"lease expired on WLM-4"}},
		Context: Context{
			Decisions: []Ref{{Ref: "decision://decision-1", Title: "限流算法选型", Extra: "accepted"}},
			Findings:  []Ref{{Ref: "finding://finding-1", Title: "令牌桶更合适"}},
			Artifacts: []Ref{{Ref: "artifact://artifact-2", Title: "限流设计", Extra: "v2"}},
			Comments:  []Ref{{Ref: "event://ev-1", Title: "评论：记得加指标", Extra: "owner"}},
		},
	}
}

// A work item without a policy says so instead of rendering an empty template
// section, and the full round carries the workflow step list.
func TestAssembleFullRoundWithoutPolicyStatesIt(t *testing.T) {
	in := fullInput()
	in.Policy = Policy{}
	in.Remaining = []string{"当前步骤：implement", "待完成步骤：verify"}
	p, err := Assemble(in)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for _, want := range []string{"## 工作流策略正文", noPolicyBody, "## 工作流步骤", "待完成步骤：verify"} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("full prompt misses %q:\n%s", want, p.Text)
		}
	}
	// With a policy attached the fallback never appears.
	withPolicy, err := Assemble(fullInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withPolicy.Text, noPolicyBody) {
		t.Fatalf("attached policy rendered the fallback text:\n%s", withPolicy.Text)
	}
}

// The same input must assemble the same text and hash: the stored hash is the
// only proof of what the agent received.
func TestAssembleIsDeterministic(t *testing.T) {
	first, err := Assemble(fullInput())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	second, err := Assemble(fullInput())
	if err != nil {
		t.Fatalf("assemble again: %v", err)
	}
	if first.Text != second.Text || first.Hash != second.Hash {
		t.Fatalf("assembly is not deterministic:\n%q\n%q", first.Text, second.Text)
	}
	if len(first.Hash) != 64 {
		t.Fatalf("hash = %q, want a sha256 hex digest", first.Hash)
	}
}

// The first round carries everything a fresh session needs, including the
// rendered policy body.
func TestAssembleFullRoundCarriesTheBrief(t *testing.T) {
	p, err := Assemble(fullInput())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if p.Mode != ModeFull {
		t.Fatalf("mode = %q, want %q", p.Mode, ModeFull)
	}
	for _, want := range []string{
		"## 任务", "## 工作流策略正文", "## 当前状态", "## 上下文引用", "## 汇报协议",
		"按 implement 步骤实现，目标：实现登录限流。", // rendered template
		"### 验收标准", "超限返回 429",
		"decision://decision-1", "artifact://artifact-2",
		"devsys run complete",
	} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("full prompt misses %q:\n%s", want, p.Text)
		}
	}
	refs := strings.Join(p.Refs, ",")
	if !strings.Contains(refs, "decision://decision-1") || !strings.Contains(refs, "workflow://feature-development") {
		t.Fatalf("refs = %v, want context pointers and the policy", p.Refs)
	}
}

// A continuation round must not resend the brief: that is the whole point of
// multi-round sessions (§4.8).
func TestAssembleContinuationSendsOnlyTheDelta(t *testing.T) {
	in := fullInput()
	in.Round = 2
	in.Changes = []Change{{Kind: "comment", Ref: "event://ev-9", Detail: "改了下限流阈值"}}
	in.Remaining = []string{"step: verify", "gate: verification artifact missing"}
	p, err := Assemble(in)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if p.Mode != ModeContinuation {
		t.Fatalf("mode = %q, want %q", p.Mode, ModeContinuation)
	}
	for _, want := range []string{"## 上一轮以来的变化", "event://ev-9", "## 未完成项", "gate: verification artifact missing"} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("continuation misses %q:\n%s", want, p.Text)
		}
	}
	for _, unwanted := range []string{"## 工作流策略正文", "按 implement 步骤实现", "### 验收标准", "decision://decision-1"} {
		if strings.Contains(p.Text, unwanted) {
			t.Fatalf("continuation resent %q:\n%s", unwanted, p.Text)
		}
	}
	full, err := Assemble(fullInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Text) >= len(full.Text) {
		t.Fatalf("continuation is %d bytes, full is %d: it must be smaller", len(p.Text), len(full.Text))
	}
}

// A missing template variable is an error, never a silently empty string.
func TestAssembleFailsOnUnrenderedTemplate(t *testing.T) {
	in := fullInput()
	in.Policy.Body = "目标：{{title}}，负责人：{{owner}}"
	in.Policy.Vars = map[string]string{"title": "实现登录限流"}
	if _, err := Assemble(in); err == nil {
		t.Fatal("assembly accepted a template with an unknown variable")
	}
}

func TestAssembleRejectsBadInput(t *testing.T) {
	in := fullInput()
	in.Round = 0
	if _, err := Assemble(in); err == nil {
		t.Fatal("round 0 accepted")
	}
	in = fullInput()
	in.Task.ID = ""
	if _, err := Assemble(in); err == nil {
		t.Fatal("empty work item accepted")
	}
}

// Empty context and no changes still render a complete document.
func TestAssembleHandlesEmptySections(t *testing.T) {
	in := Input{Round: 1, Task: Task{ID: "WLM-1", Title: "小事"}}
	p, err := Assemble(in)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !strings.Contains(p.Text, "## 上下文引用\n\n（无）") {
		t.Fatalf("empty context section = %q", p.Text)
	}
	in.Round = 3
	cont, err := Assemble(in)
	if err != nil {
		t.Fatalf("assemble continuation: %v", err)
	}
	if !strings.Contains(cont.Text, "（无）") || len(cont.Refs) != 0 {
		t.Fatalf("empty continuation = %q refs=%v", cont.Text, cont.Refs)
	}
}
