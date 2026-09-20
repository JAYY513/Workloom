package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/project"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

// promptFixture initializes a real project with a policy, a work item and a
// run, and returns the root, the run id and the work item id.
func promptFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	if _, err := project.Init(root, project.Options{Now: time.Now().UTC()}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	policy := "---\nid: feature\nname: 功能开发\nversion: 1\nsteps:\n" +
		"  - id: implement\n    type: execute\n    required: true\n" +
		"  - id: verify\n    type: verify\n    required: true\n" +
		"limits:\n  max_attempts: 3\n---\n" +
		"目标：{{workitem.title}}（项目 {{project.name}}，步骤 {{step}}）。\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workflows", "feature.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	items := workitem.New(root)
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: "实现登录限流", Description: "给登录接口加限流。",
		Status: domain.StatusReady, AcceptanceCriteria: []string{"超限返回 429"},
		Workflow:  &domain.WorkflowInstance{ID: "feature", Step: "implement", StepEnteredAt: now},
		CreatedAt: now, UpdatedAt: now,
	}
	id, err := items.Create(ctx, wi, "WLM")
	if err != nil {
		t.Fatalf("work item create: %v", err)
	}
	runID, err := run.New(root).Create(ctx, &domain.Run{
		ProjectID: "demo", WorkItemID: id, WorkflowID: "feature",
		Status: "created", Attempt: 1, Phase: "building_prompt", StartedAt: now,
	})
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	return root, runID, id
}

// The first round carries the task brief with the policy body rendered from the
// run's own facts.
func TestRunPromptFullRound(t *testing.T) {
	root, runID, _ := promptFixture(t)
	svc := New(root)
	view, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID, Write: true})
	if err != nil {
		t.Fatalf("run prompt: %v", err)
	}
	if view.Round != 1 || view.Mode != "full" {
		t.Fatalf("round/mode = %d/%s, want 1/full", view.Round, view.Mode)
	}
	for _, want := range []string{
		"## 任务", "实现登录限流", "### 验收标准", "超限返回 429",
		"## 工作流策略正文", "目标：实现登录限流", "步骤 implement", // rendered template
		"## 工作流步骤", "待完成步骤：verify", // the first round carries the step list too
		"## 汇报协议", "devsys run complete", "--by <身份>",
	} {
		if !strings.Contains(view.Text, want) {
			t.Fatalf("prompt misses %q:\n%s", want, view.Text)
		}
	}
	if len(view.Refs) == 0 || !strings.Contains(strings.Join(view.Refs, ","), "workflow://feature") {
		t.Fatalf("refs = %v, want the policy pointer", view.Refs)
	}
	if view.Path == "" {
		t.Fatal("write requested but no path reported")
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.Path)))
	if err != nil {
		t.Fatalf("read written prompt: %v", err)
	}
	if string(body) != view.Text {
		t.Fatal("written prompt differs from the assembled text")
	}
	if !strings.HasPrefix(view.Path, ".devsys/local/") {
		t.Fatalf("path = %q, want a path under .devsys/local/", view.Path)
	}
}

// Assembly is deterministic: the same run yields the same text and hash, which
// is what makes the stored hash meaningful.
func TestRunPromptIsReplayable(t *testing.T) {
	root, runID, _ := promptFixture(t)
	svc := New(root)
	first, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash || first.Text != second.Text {
		t.Fatalf("assembly is not deterministic:\n%q\n%q", first.Text, second.Text)
	}
	if len(first.Hash) != 64 {
		t.Fatalf("hash = %q", first.Hash)
	}
}

// A continuation round drops the brief and carries the delta instead.
func TestRunPromptContinuationDropsTheBrief(t *testing.T) {
	root, runID, _ := promptFixture(t)
	svc := New(root)
	full, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID, Round: 1})
	if err != nil {
		t.Fatal(err)
	}
	cont, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID, Round: 2})
	if err != nil {
		t.Fatalf("continuation: %v", err)
	}
	if cont.Mode != "continuation" || cont.Round != 2 {
		t.Fatalf("round/mode = %d/%s, want 2/continuation", cont.Round, cont.Mode)
	}
	if strings.Contains(cont.Text, "## 工作流策略正文") || strings.Contains(cont.Text, "目标：实现登录限流") {
		t.Fatalf("continuation resent the brief:\n%s", cont.Text)
	}
	for _, want := range []string{"## 上一轮以来的变化", "## 未完成项", "待完成步骤：verify"} {
		if !strings.Contains(cont.Text, want) {
			t.Fatalf("continuation misses %q:\n%s", want, cont.Text)
		}
	}
	if len(cont.Text) >= len(full.Text) {
		t.Fatalf("continuation (%d bytes) is not smaller than the full round (%d bytes)", len(cont.Text), len(full.Text))
	}
}

// The next round number comes from the stream: a session survives a restart
// because its bookkeeping is the evidence trail itself.
func TestRunPromptNextRoundComesFromTheStream(t *testing.T) {
	root, runID, _ := promptFixture(t)
	svc := New(root)
	view, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if view.Round != 1 {
		t.Fatalf("round = %d, want 1 with an empty stream", view.Round)
	}
	// Simulate a completed first round: the round record is what the next
	// attempt counts from.
	record := `{"t":"2026-09-18T10:00:00Z","type":"round","n":1,"mode":"full","prompt_hash":"abc"}` + "\n"
	path := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err = svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if view.Round != 2 || view.Mode != "continuation" {
		t.Fatalf("round/mode = %d/%s, want 2/continuation", view.Round, view.Mode)
	}
	// Changes since the previous round appear in the continuation.
	events := `{"schema_version":1,"id":"ev-1","project_id":"demo","type":"comment","subject":{"type":"workitem","id":"WLM-1"},"time":"2026-09-18T10:05:00Z","actor":"owner","content":"记得加指标"}
`
	if err := os.WriteFile(filepath.Join(root, ".devsys", "events", "2026-09.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err = svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Text, "记得加指标") {
		t.Fatalf("continuation misses the new comment:\n%s", view.Text)
	}
}

// A torn tail in the stream must not break the read side: the writer repairs it
// on its next open.
func TestRunPromptToleratesTornStreamTail(t *testing.T) {
	root, runID, _ := promptFixture(t)
	path := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	body := `{"t":"2026-09-18T10:00:00Z","type":"round","n":4,"mode":"full"}` + "\n" + `{"t":"2026-09-18T10:01:00Z","type":"out`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := New(root).RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatalf("run prompt with a torn tail: %v", err)
	}
	if view.Round != 5 {
		t.Fatalf("round = %d, want 5 (the complete records still count)", view.Round)
	}
}

// A broken policy blocks a first round (the claim path fails closed the same
// way) but a continuation may proceed on the last-known-good copy.
func TestRunPromptPolicyFailureSemantics(t *testing.T) {
	root, runID, _ := promptFixture(t)
	policyPath := filepath.Join(root, ".devsys", "workflows", "feature.md")
	good, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(root)
	if _, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID}); err != nil {
		t.Fatalf("prime the last-known-good copy: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte("---\nid: feature\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID, Round: 1}); err == nil {
		t.Fatal("a broken policy still produced a full prompt")
	} else if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %v, want a classified invalid-policy failure", err)
	}
	view, err := svc.RunPrompt(context.Background(), RunPromptRequest{RunID: runID, Round: 2})
	if err != nil {
		t.Fatalf("continuation with a broken policy: %v", err)
	}
	if len(view.Notices) == 0 || !strings.Contains(strings.Join(view.Notices, " "), "last-known-good") {
		t.Fatalf("notices = %v, want the last-known-good warning", view.Notices)
	}
	if err := os.WriteFile(policyPath, good, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The work item's specification body is part of the brief when it exists.
func TestRunPromptIncludesSpecBody(t *testing.T) {
	root, runID, wiID := promptFixture(t)
	spec := "# 规格\n\n每秒最多 5 次。\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "specs", wiID+".md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := New(root).RunPrompt(context.Background(), RunPromptRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Text, "### 任务规格") || !strings.Contains(view.Text, "每秒最多 5 次。") {
		t.Fatalf("prompt misses the spec body:\n%s", view.Text)
	}
}

func TestRunPromptRejectsUnknownRun(t *testing.T) {
	root, _, _ := promptFixture(t)
	if _, err := New(root).RunPrompt(context.Background(), RunPromptRequest{RunID: "run-19700101-1"}); err == nil {
		t.Fatal("unknown run accepted")
	} else if ae, ok := err.(*Error); !ok || ae.Class() != KindPrecondition {
		t.Fatalf("error = %v, want a precondition failure", err)
	}
}
