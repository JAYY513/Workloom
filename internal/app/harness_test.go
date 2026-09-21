package app

import (
	"context"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/harness"
	"github.com/JAYY513/Workloom/internal/workitem"
)

// harnessByName keeps the test honest about what this machine has installed.
func harnessByName(name string) (harness.Adapter, bool) { return harness.ByName(name) }

// workitemStore is the store the fixtures write through.
func workitemStore(root string) *workitem.Store { return workitem.New(root) }

func assertRunRefused(t *testing.T, svc *Service, workitemID, runID, wantErr string) {
	t.Helper()
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	if view.Run.Status != RunFailed {
		t.Fatalf("run status = %q, want failed", view.Run.Status)
	}
	found := false
	for _, e := range view.Run.Result.Errors {
		if strings.Contains(e, wantErr) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("run errors = %v, want %q", view.Run.Result.Errors, wantErr)
	}
	evs, err := events.New(svc.Root).Read(context.Background(), events.Filter{Type: "run_failed"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	ok := false
	for _, ev := range evs {
		if ev.Subject.ID == runID {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("no run_failed event for %s", runID)
	}
	wi, err := svc.items().Get(context.Background(), workitemID)
	if err != nil {
		t.Fatalf("work item: %v", err)
	}
	if wi.Status != domain.StatusRetryQueued {
		t.Fatalf("work item status = %s, want retry_queued (the lease must not be left unclaimed in_progress)", wi.Status)
	}
}

// A declared harness that is not installed is refused with the probe's own
// words: devsys never quietly runs a different harness.
func TestRunExecRefusesUnavailableHarness(t *testing.T) {
	_, svc, workitemID, runID := verifyFixture(t)
	// claude is the one harness this machine does not have; the assertion is
	// written against the probe so the test stays honest if that changes.
	if adapter, ok := harnessByName("claude"); ok {
		if avail, err := adapter.Probe(context.Background()); err == nil && avail.Installed {
			t.Skip("claude is installed on this machine; the unavailable-harness path needs one that is not")
		}
	}
	_, err := svc.RunExec(context.Background(), RunExecRequest{
		RunID: runID, Harness: "claude", Actor: "ops", Reason: "attempt",
	})
	if err == nil {
		t.Fatal("an unavailable harness was used")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error = %v, want the probe's explanation", err)
	}
	assertRunRefused(t, svc, workitemID, runID, "not available")
}

func TestRunExecRejectsUnknownOrMisusedHarness(t *testing.T) {
	cases := []struct {
		name string
		req  RunExecRequest
		want string
	}{
		{"unknown", RunExecRequest{Harness: "no-such-harness", Actor: "ops", Reason: "attempt"}, "unknown harness"},
		{"shell", RunExecRequest{Harness: "shell", Actor: "ops", Reason: "attempt"}, "explicit command"},
		{"both", RunExecRequest{Harness: "codex", Argv: []string{"echo", "hi"}, Actor: "ops", Reason: "attempt"}, "not both"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, svc, workitemID, runID := verifyFixture(t)
			tc.req.RunID = runID
			if _, err := svc.RunExec(context.Background(), tc.req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			assertRunRefused(t, svc, workitemID, runID, tc.want)
		})
	}
}

// A work item that names its harness is dispatched through that adapter: the
// spawned attempt carries --harness instead of a shell command.
func TestDispatchUsesAssignedHarness(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "harnessed", Priority: 5})
	items := workitemStore(root)
	wi, raw, err := items.ReadSnapshot(context.Background(), "WLM-1")
	if err != nil {
		t.Fatal(err)
	}
	name := "codex"
	wi.AssignedHarness = &name
	if err := items.Update(context.Background(), wi, raw); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("started = %+v, want one attempt", report.Started)
	}
	joined := strings.Join(rec.argv, " ")
	if !strings.Contains(joined, "--harness codex") {
		t.Fatalf("spawn argv = %v, want the assigned harness", rec.argv)
	}
	if strings.Contains(joined, " -- ") {
		t.Fatalf("spawn argv = %v, want no shell command for a harnessed item", rec.argv)
	}
	if report.Started[0].Command != "harness:codex" {
		t.Fatalf("reported command = %q, want the harness named", report.Started[0].Command)
	}
	view, err := svc.RunGet(context.Background(), report.Started[0].RunID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Agent.Harness != "codex" {
		t.Fatalf("run harness = %q, want it stamped after spawn", view.Run.Agent.Harness)
	}
}

func assignHarness(t *testing.T, root, id, name string) {
	t.Helper()
	items := workitemStore(root)
	wi, raw, err := items.ReadSnapshot(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	n := name
	wi.AssignedHarness = &n
	if err := items.Update(context.Background(), wi, raw); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchRefusesShellAssignedHarnessWithoutClaiming(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "shellish", Priority: 5})
	assignHarness(t, root, "WLM-1", "shell")
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err == nil {
		t.Fatalf("dispatch started a shell harness: %+v", report)
	}
	if !strings.Contains(err.Error(), "explicit command") && !strings.Contains(err.Error(), "started nothing") {
		t.Fatalf("error = %v, want the shell adapter refused", err)
	}
	wi := itemState(t, root, "WLM-1")
	if wi.SchedulingState == domain.SchedulingClaimed || wi.SchedulingState == domain.SchedulingRunning {
		t.Fatalf("the item was claimed anyway: %s/%s", wi.Status, wi.SchedulingState)
	}
}

func TestDispatchRefusesUnknownAssignedHarnessWithoutClaiming(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "unknown", Priority: 5})
	assignHarness(t, root, "WLM-1", "no-such-harness")
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err == nil {
		t.Fatalf("dispatch started an unknown harness: %+v", report)
	}
	if !strings.Contains(err.Error(), "unknown harness") && !strings.Contains(err.Error(), "started nothing") {
		t.Fatalf("error = %v, want unknown harness refused", err)
	}
	wi := itemState(t, root, "WLM-1")
	if wi.SchedulingState == domain.SchedulingClaimed || wi.SchedulingState == domain.SchedulingRunning {
		t.Fatalf("the item was claimed anyway: %s/%s", wi.Status, wi.SchedulingState)
	}
}

func TestWorkitemUpdateRejectsUnknownHarness(t *testing.T) {
	_, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	bad := "nope"
	if _, err := svc.WorkitemUpdate(context.Background(), "WLM-1", UpdateWorkitemRequest{AssignedHarness: &bad, Actor: "tester", Reason: "audit coverage"}); err == nil || !strings.Contains(err.Error(), "unknown harness") {
		t.Fatalf("error = %v, want unknown harness", err)
	}
}
