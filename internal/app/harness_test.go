package app

import (
	"context"
	"strings"
	"testing"

	"workloom/internal/domain"
	"workloom/internal/harness"
	"workloom/internal/workitem"
)

// harnessByName keeps the test honest about what this machine has installed.
func harnessByName(name string) (harness.Adapter, bool) { return harness.ByName(name) }

// workitemStore is the store the fixtures write through.
func workitemStore(root string) *workitem.Store { return workitem.New(root) }

// A declared harness that is not installed is refused with the probe's own
// words: devsys never quietly runs a different harness.
func TestRunExecRefusesUnavailableHarness(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
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
}

func TestRunExecRejectsUnknownOrMisusedHarness(t *testing.T) {
	_, svc, _, runID := verifyFixture(t)
	if _, err := svc.RunExec(context.Background(), RunExecRequest{
		RunID: runID, Harness: "no-such-harness", Actor: "ops", Reason: "attempt",
	}); err == nil || !strings.Contains(err.Error(), "unknown harness") {
		t.Fatalf("error = %v, want an unknown-harness usage error", err)
	}
	if _, err := svc.RunExec(context.Background(), RunExecRequest{
		RunID: runID, Harness: "shell", Actor: "ops", Reason: "attempt",
	}); err == nil || !strings.Contains(err.Error(), "explicit command") {
		t.Fatalf("error = %v, want the shell adapter pointed at -- <command>", err)
	}
	if _, err := svc.RunExec(context.Background(), RunExecRequest{
		RunID: runID, Harness: "codex", Argv: []string{"echo", "hi"}, Actor: "ops", Reason: "attempt",
	}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error = %v, want the harness/argv conflict", err)
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
}
