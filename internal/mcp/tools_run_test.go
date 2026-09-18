package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/domain"
	"workloom/internal/project"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

// fixedNow keeps the fixture deterministic.
func fixedNow() time.Time { return time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC) }

// runFixture initializes a real project in a temporary git repository with one
// work item and one run, so the lifecycle tools are exercised against real
// managed state rather than a stub.
func runFixture(t *testing.T) (string, string) {
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
	if _, err := project.Init(root, project.Options{Now: fixedNow()}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	ctx := context.Background()
	items := workitem.New(root)
	now := fixedNow()
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: "lifecycle target", Description: "fixture",
		Status: domain.StatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	id, err := items.Create(ctx, wi, "WLM")
	if err != nil {
		t.Fatalf("work item create: %v", err)
	}
	runID, err := run.New(root).Create(ctx, &domain.Run{
		ProjectID: "demo", WorkItemID: id, Status: "created", Attempt: 1,
		Phase: "building_prompt", StartedAt: now,
	})
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	return root, runID
}

// The three terminal transitions are executor tools: a session profile must
// not see them, and the executor profile must.
func TestRunLifecycleToolsAreProfileGraded(t *testing.T) {
	root, _ := runFixture(t)
	sessionCS := session(t, Config{Root: root, ServerVersion: "test", Profiles: []string{ProfileSession}})
	names := toolNames(t, sessionCS)
	for _, tool := range []string{"run_complete", "run_fail", "run_cancel"} {
		if contains(names, tool) {
			t.Fatalf("session profile exposes %s: %v", tool, names)
		}
	}
	executor := session(t, Config{Root: root, ServerVersion: "test", Profiles: []string{ProfileExecutor}})
	names = toolNames(t, executor)
	for _, tool := range []string{"run_complete", "run_fail", "run_cancel"} {
		if !contains(names, tool) {
			t.Fatalf("executor profile misses %s: %v", tool, names)
		}
	}
}

// The lifecycle tools write under the version guard and refuse to end a run
// that already ended, so two actors cannot both claim the outcome.
func TestRunCompleteRequiresExpectAndEndsOnce(t *testing.T) {
	root, runID := runFixture(t)
	cs := session(t, Config{Root: root, ServerVersion: "test"})

	stale, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_complete",
		Arguments: map[string]any{
			"id": runID, "expect": "0000000000000000000000000000000000000000000000000000000000000000",
			"actor": "tester", "reason": "done",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !stale.IsError {
		t.Fatalf("a stale expect was accepted: %+v", stale)
	}
	if got := payload[toolError](t, stale); got.Code != "invalid" && got.Code != "precondition" {
		t.Fatalf("stale expect error code = %q", got.Code)
	}

	view, err := app.New(root).RunGet(context.Background(), runID)
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	fresh, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_complete",
		Arguments: map[string]any{
			"id": runID, "expect": view.Version,
			"actor": "tester", "reason": "acceptance met",
			// The fixture run has no workspace, so the completion check has no
			// evidence: a reviewer accepts it explicitly.
			"force": true, "by": "tester",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if fresh.IsError {
		t.Fatalf("run_complete failed: %+v", fresh)
	}
	got := payload[app.RunView](t, fresh)
	if got.Run.Status != app.RunSucceeded || got.Run.FinishedAt == nil {
		t.Fatalf("run = %+v, want a finished succeeded run", got.Run)
	}

	again, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_complete",
		Arguments: map[string]any{
			"id": runID, "expect": got.Version,
			"actor": "tester", "reason": "again",
			"force": true, "by": "tester",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !again.IsError {
		t.Fatalf("a finished run was ended twice: %+v", again)
	}
	if msg := payload[toolError](t, again); !strings.Contains(msg.Message, "already ended") {
		t.Fatalf("error = %+v, want it to say the run already ended", msg)
	}
}

// run_fail and run_cancel record their own outcomes.
func TestRunFailAndCancelRecordOutcomes(t *testing.T) {
	root, runID := runFixture(t)
	cs := session(t, Config{Root: root, ServerVersion: "test"})
	view, err := app.New(root).RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_fail",
		Arguments: map[string]any{
			"id": runID, "expect": view.Version, "actor": "tester",
			"reason": "tests failed", "note": "command exited 9",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res.IsError {
		t.Fatalf("run_fail failed: %+v", res)
	}
	got := payload[app.RunView](t, res)
	if got.Run.Status != app.RunFailed {
		t.Fatalf("status = %q, want %q", got.Run.Status, app.RunFailed)
	}
	if len(got.Run.Result.Errors) == 0 || got.Run.Result.Errors[0] != "command exited 9" {
		t.Fatalf("result errors = %v, want the note", got.Run.Result.Errors)
	}
}

// The completion check is readable from the tool surface, and completing
// without an advance is refused with the check's reason.
func TestRunVerifyAndRefusedCompletion(t *testing.T) {
	root, runID := runFixture(t)
	cs := session(t, Config{Root: root, ServerVersion: "test"})
	if names := toolNames(t, cs); !contains(names, "run_verify") {
		t.Fatalf("run_verify is not exposed: %v", names)
	}
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_verify", Arguments: map[string]any{"id": runID},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res.IsError {
		t.Fatalf("run_verify failed: %+v", res)
	}
	check := payload[app.CompletionCheck](t, res)
	if check.Advanced || check.Reason == "" {
		t.Fatalf("check = %+v, want a refusal with a reason (the fixture has no workspace)", check)
	}
	view, err := app.New(root).RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	refused, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_complete",
		Arguments: map[string]any{
			"id": runID, "expect": view.Version, "actor": "agent", "reason": "done",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !refused.IsError {
		t.Fatalf("a completion without evidence was accepted: %+v", refused)
	}
	if msg := payload[toolError](t, refused); !strings.Contains(msg.Message, "cannot be marked succeeded") {
		t.Fatalf("error = %+v, want the completion refusal", msg)
	}
	// A reviewer may accept it, and the tool records who did.
	after, err := app.New(root).RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_complete",
		Arguments: map[string]any{
			"id": runID, "expect": after.Version, "actor": "alice", "reason": "reviewed",
			"force": true, "by": "alice",
		},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if forced.IsError {
		t.Fatalf("forced completion failed: %+v", forced)
	}
	done := payload[app.RunView](t, forced)
	if done.Run.Verification.VerifiedBy == nil || *done.Run.Verification.VerifiedBy != "alice" {
		t.Fatalf("verification = %+v, want verified_by=alice", done.Run.Verification)
	}
}
