package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"workloom/internal/domain"
)

// verifyFixture is a project with one claimed attempt bound to a workspace:
// the state the completion check inspects.
func verifyFixture(t *testing.T) (root string, svc *Service, workitemID, runID string) {
	t.Helper()
	root, svc = dispatchFixture(t, "", &domain.WorkItem{Title: "verify target", Priority: 5})
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("started = %+v, want one attempt", report.Started)
	}
	return root, svc, report.Started[0].WorkitemID, report.Started[0].RunID
}

func commitInWorkspace(t *testing.T, runID string, svc *Service, name string) {
	t.Helper()
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	ws := view.Run.Workspace.Path
	if ws == "" {
		t.Fatal("run has no workspace")
	}
	if err := os.WriteFile(filepath.Join(ws, name), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.email=a@b.c", "-c", "user.name=agent", "commit", "-q", "-m", "work"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// An attempt that committed nothing cannot be marked succeeded: the refusal is
// recorded, the work item goes to review, and the run stays open.
func TestCompletionRefusedWithoutAdvance(t *testing.T) {
	root, svc, workitemID, runID := verifyFixture(t)
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Claim.HeadSHA == "" {
		t.Fatal("the claim head was not recorded when the workspace was bound")
	}
	_, err = svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: RunSucceeded, Actor: "agent", Reason: "done",
	})
	if err == nil {
		t.Fatal("an attempt that advanced nothing was marked succeeded")
	}
	if !strings.Contains(err.Error(), "cannot be marked succeeded") {
		t.Fatalf("error = %v, want the refusal explained", err)
	}
	after, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if isTerminalRun(after.Run.Status) {
		t.Fatalf("run status = %q, want it left open for review", after.Run.Status)
	}
	if after.Run.Verification.Advanced == nil || *after.Run.Verification.Advanced {
		t.Fatalf("verification = %+v, want advanced=false recorded", after.Run.Verification)
	}
	if after.Run.Verification.HeadSHAAtComplete == "" {
		t.Fatalf("verification = %+v, want the head it stopped at", after.Run.Verification)
	}
	if got := itemState(t, root, workitemID).Status; got != domain.StatusReview {
		t.Fatalf("work item status = %q, want review (the human queue)", got)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "completion_refused"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !strings.Contains(events[0].Content, "nothing was committed") {
		t.Fatalf("completion_refused events = %+v, want the reason with both heads", events)
	}
}

// With a commit on the attempt's branch the completion goes through and the
// evidence says which head it verified.
func TestCompletionVerifiedAfterAdvance(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
	commitInWorkspace(t, runID, svc, "work.txt")
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	check, err := svc.RunVerify(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Advanced || check.CurrentHead == check.ClaimHead {
		t.Fatalf("check = %+v, want an advance", check)
	}
	done, err := svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: RunSucceeded, Actor: "agent", Reason: "done",
	})
	if err != nil {
		t.Fatalf("run finish: %v", err)
	}
	if done.Run.Status != RunSucceeded {
		t.Fatalf("status = %q, want succeeded", done.Run.Status)
	}
	if done.Run.Verification.Advanced == nil || !*done.Run.Verification.Advanced {
		t.Fatalf("verification = %+v, want advanced=true", done.Run.Verification)
	}
	if done.Run.Verification.HeadSHAAtComplete != check.CurrentHead {
		t.Fatalf("verified head = %q, want %q", done.Run.Verification.HeadSHAAtComplete, check.CurrentHead)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "completion_verified"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("completion_verified events = %d, want 1", len(events))
	}
}

// A reviewer can accept a refused completion, and the exception carries their
// name.
func TestCompletionForcedByReviewer(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: RunSucceeded, Actor: "agent", Reason: "done",
	}); err == nil {
		t.Fatal("the first completion was not refused")
	}
	refused, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	done, err := svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: refused.Version, Outcome: RunSucceeded,
		Actor: "alice", Reason: "reviewed by hand", Force: true, By: "alice",
	})
	if err != nil {
		t.Fatalf("forced completion: %v", err)
	}
	if done.Run.Verification.VerifiedBy == nil || *done.Run.Verification.VerifiedBy != "alice" {
		t.Fatalf("verification = %+v, want verified_by=alice", done.Run.Verification)
	}
	if done.Run.Verification.Advanced == nil || *done.Run.Verification.Advanced {
		t.Fatalf("verification = %+v, want advanced=false (the check did refuse)", done.Run.Verification)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "completion_overridden"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !strings.Contains(events[0].Content, "alice") {
		t.Fatalf("completion_overridden events = %+v, want the reviewer named", events)
	}
}

// Forcing without a reviewer is a usage error: an exception has to be
// traceable.
func TestCompletionForceRequiresReviewer(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: RunSucceeded, Actor: "agent", Reason: "done", Force: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("error = %v, want a usage error naming --by", err)
	}
}

// An attempt without a workspace has no claim head either, and "no evidence"
// is never "verified".
func TestCompletionRefusedWithoutEvidence(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "ad-hoc", Priority: 5})
	// A run created directly, without a claim or a workspace.
	view, err := svc.RunCreate(context.Background(), CreateRunRequest{
		WorkItemID: "WLM-1", Actor: "test", Reason: "fixture",
	})
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	check, err := svc.RunVerify(context.Background(), view.Run.ID)
	if err != nil {
		t.Fatalf("run verify: %v", err)
	}
	if check.Advanced || check.Reason == "" {
		t.Fatalf("check = %+v, want a refusal with a reason", check)
	}
	_, err = svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: view.Run.ID, Expect: view.Version, Outcome: RunSucceeded, Actor: "agent", Reason: "done",
	})
	if err == nil {
		t.Fatal("a run without evidence was marked succeeded")
	}
	if got := itemState(t, root, "WLM-1").Status; got != domain.StatusReview {
		t.Fatalf("work item status = %q, want review", got)
	}
}

// The verification is read-only.
func TestRunVerifyDoesNotWrite(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
	before, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.RunVerify(context.Background(), runID); err != nil {
			t.Fatalf("run verify: %v", err)
		}
	}
	after, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Run.Status != before.Run.Status {
		t.Fatalf("run verify changed the run: %s -> %s", before.Version, after.Version)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if strings.HasPrefix(ev.Type, "completion_") {
			t.Fatalf("run verify wrote %s", ev.Type)
		}
	}
}
