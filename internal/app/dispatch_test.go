package app

import (
	"context"
	"errors"
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

// dispatchFixture is a project with a policy, a config carrying a dispatch
// command, and the work items the test asks for.
func dispatchFixture(t *testing.T, concurrency string, items ...*domain.WorkItem) (string, *Service) {
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
	now := time.Now().UTC()
	if _, err := project.Init(root, project.Options{Now: now}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	policy := "---\nid: conc\nname: 并发策略\nversion: 1\nsteps:\n" +
		"  - id: implement\n    type: execute\n    required: true\n"
	if concurrency != "" {
		policy += concurrency
	}
	policy += "limits:\n  max_attempts: 3\n---\n实现：{{workitem.title}}\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workflows", "conc.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = append(config, []byte("dispatch_command: \"echo dispatched\"\n")...)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	commit := exec.Command("git", "-C", root,
		"-c", "user.email=devsys@example.test", "-c", "user.name=devsys test",
		"commit", "-q", "-m", "fixture")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	for _, item := range items {
		item.ProjectID = "demo"
		item.Type = "task"
		item.Status = domain.StatusReady
		item.Workflow = &domain.WorkflowInstance{ID: "conc", Step: "implement", StepEnteredAt: now}
		item.CreatedAt, item.UpdatedAt = now, now
		if _, err := workitem.New(root).Create(context.Background(), item, "WLM"); err != nil {
			t.Fatalf("create %s: %v", item.ID, err)
		}
	}
	return root, New(root)
}

// recorder captures what a tick tried to start.
type recorder struct {
	root    string
	runID   string
	argv    []string
	logPath string
	calls   int
}

func (r *recorder) spawn(root, runID string, argv []string, logPath string) (int, error) {
	r.calls++
	r.root, r.runID, r.argv, r.logPath = root, runID, argv, logPath
	return 4242, nil
}

// releaseClaim ends the claim a tick made, the way a finished attempt does.
func releaseClaim(t *testing.T, svc *Service, workitemID string) {
	t.Helper()
	lease, err := workitem.New(svc.Root).LeaseInspection(context.Background(), workitemID)
	if err != nil {
		t.Fatalf("lease inspection: %v", err)
	}
	if _, err := svc.WorkitemRelease(context.Background(), workitemID, lease.Owner, lease.Token, "ops", "attempt finished", ""); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func itemState(t *testing.T, root, id string) *domain.WorkItem {
	t.Helper()
	wi, err := workitem.New(root).Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return wi
}

// A tick starts one attempt per available slot, in the spec's order, and a
// repeated tick starts nothing: the claim is what makes it idempotent.
func TestDispatchStartsInOrderAndIsIdempotent(t *testing.T) {
	root, svc := dispatchFixture(t, "",
		&domain.WorkItem{Title: "低优先级", Priority: 5},
		&domain.WorkItem{Title: "高优先级", Priority: 9},
	)
	rec := &recorder{}
	first, err := svc.Dispatch(context.Background(), DispatchRequest{
		Actor: "ops", Reason: "tick", Spawn: rec.spawn,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(first.Started) != 1 {
		t.Fatalf("started %d attempts, want 1 (the default global cap)", len(first.Started))
	}
	started := first.Started[0]
	if started.WorkitemID != "WLM-2" {
		t.Fatalf("started %s, want the higher priority item WLM-2", started.WorkitemID)
	}
	if rec.calls != 1 || !strings.Contains(strings.Join(rec.argv, " "), "run exec --id "+started.RunID) {
		t.Fatalf("spawn argv = %v, want a run exec for %s", rec.argv, started.RunID)
	}
	if !strings.HasSuffix(rec.logPath, started.RunID+".exec.log") {
		t.Fatalf("log path = %q, want the attempt log", rec.logPath)
	}
	// The claim, the workspace binding and the running state are all recorded.
	claimed := itemState(t, root, "WLM-2")
	if claimed.SchedulingState != domain.SchedulingClaimed || claimed.Status != domain.StatusInProgress {
		t.Fatalf("work item = %s/%s, want the claim to hold it (in_progress/claimed)", claimed.Status, claimed.SchedulingState)
	}
	view, err := svc.RunGet(context.Background(), started.RunID)
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	if view.Run.Workspace.Path == "" || view.Run.Workspace.Worktree == "" {
		t.Fatalf("run workspace = %+v, want the prepared workspace", view.Run.Workspace)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run status = %q, want running (the claim opens the attempt)", view.Run.Status)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "run_dispatched"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Subject.ID != started.RunID {
		t.Fatalf("run_dispatched events = %+v, want one for %s", events, started.RunID)
	}
	// The attempt finishes (its lease is released), which frees the slot: a
	// second tick then starts the remaining item, and a third has nothing to
	// do because that claim is still held.
	releaseClaim(t, svc, "WLM-2")
	second, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if len(second.Started) != 1 || second.Started[0].WorkitemID != "WLM-1" {
		t.Fatalf("second tick started %+v, want the remaining item", second.Started)
	}
	third, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("third tick: %v", err)
	}
	if len(third.Started) != 0 {
		t.Fatalf("third tick started %+v, want nothing (idempotent)", third.Started)
	}
	if rec.calls != 2 {
		t.Fatalf("spawn called %d times, want 2", rec.calls)
	}
}

// A declared concurrency bound is enforced, and the skipped candidates say
// which bound stopped them.
func TestDispatchEnforcesConcurrencyCap(t *testing.T) {
	root, svc := dispatchFixture(t, "concurrency:\n  global: 1\n",
		&domain.WorkItem{Title: "one", Priority: 9},
		&domain.WorkItem{Title: "two", Priority: 5},
		&domain.WorkItem{Title: "three", Priority: 4},
	)
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].WorkitemID != "WLM-1" {
		t.Fatalf("started %+v, want the highest priority item only", report.Started)
	}
	capped := 0
	for _, decision := range report.Plan.Skip {
		if decision.Reason == "cap_global" {
			capped++
		}
	}
	if capped != 2 {
		t.Fatalf("cap_global skips = %d, want 2 (%+v)", capped, report.Plan.Skip)
	}
	if got := itemState(t, root, "WLM-2").SchedulingState; got == domain.SchedulingClaimed || got == domain.SchedulingRunning {
		t.Fatalf("over-cap item scheduling state = %q, want it left alone", got)
	}
}

// A work item whose dependency is unresolved is never claimed, and becomes a
// candidate once the dependency resolves.
func TestDispatchHonoursDependencies(t *testing.T) {
	dependency := &domain.WorkItem{Title: "前置", Priority: 1}
	dependent := &domain.WorkItem{Title: "后续", Priority: 9}
	dependent.Dependencies = []string{"WLM-1"}
	root, svc := dispatchFixture(t, "concurrency:\n  global: 5\n", dependency, dependent)

	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// WLM-1 is ready and dispatchable; WLM-2 is blocked by it.
	for _, attempt := range report.Started {
		if attempt.WorkitemID == "WLM-2" {
			t.Fatalf("the blocked item was started: %+v", report.Started)
		}
	}
	blocked := false
	for _, decision := range report.Plan.Skip {
		if decision.WorkitemID == "WLM-2" && decision.Reason == "blocked_by" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("skip list = %+v, want blocked_by for WLM-2", report.Plan.Skip)
	}
	if got := itemState(t, root, "WLM-2").SchedulingState; got == domain.SchedulingClaimed || got == domain.SchedulingRunning {
		t.Fatalf("blocked item scheduling state = %q, want it never claimed", got)
	}
}

// A dry run reports the plan and changes nothing.
func TestDispatchDryRunChangesNothing(t *testing.T) {
	root, svc := dispatchFixture(t, "concurrency:\n  global: 5\n",
		&domain.WorkItem{Title: "one", Priority: 9},
	)
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", DryRun: true, Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !report.DryRun || report.Recover != nil {
		t.Fatalf("report = %+v, want a dry run without recovery", report)
	}
	if len(report.Plan.Start) != 1 {
		t.Fatalf("plan = %+v, want the candidate", report.Plan)
	}
	if len(report.Started) != 0 || rec.calls != 0 {
		t.Fatalf("dry run started something: %+v", report.Started)
	}
	if got := itemState(t, root, "WLM-1").SchedulingState; got == domain.SchedulingClaimed || got == domain.SchedulingRunning {
		t.Fatalf("dry run changed the scheduling state to %q", got)
	}
	runs, err := run.New(root).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("dry run created runs: %+v", runs)
	}
}

// Without a dispatch command the tick refuses to start anything and says why,
// instead of reporting success for work it never began.
func TestDispatchWithoutCommandRefuses(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 9})
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = []byte(strings.Replace(string(config), "dispatch_command: \"echo dispatched\"\n", "", 1))
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err == nil {
		t.Fatalf("dispatch started without a command: %+v", report)
	}
	if !strings.Contains(err.Error(), "dispatch_command") {
		t.Fatalf("error = %v, want it to name the missing key", err)
	}
	if got := itemState(t, root, "WLM-1").SchedulingState; got == domain.SchedulingClaimed || got == domain.SchedulingRunning {
		t.Fatalf("the item was claimed anyway: %q", got)
	}
}

// One broken candidate does not stop the tick: it is reported and the rest of
// the queue proceeds.
func TestDispatchReportsBrokenCandidateAndContinues(t *testing.T) {
	broken := &domain.WorkItem{Title: "坏策略", Priority: 9}
	healthy := &domain.WorkItem{Title: "好策略", Priority: 5}
	root, svc := dispatchFixture(t, "", broken, healthy)
	// Point the broken item at a policy that does not parse.
	items := workitem.New(root)
	wi, raw, err := items.ReadSnapshot(context.Background(), "WLM-1")
	if err != nil {
		t.Fatal(err)
	}
	wi.Workflow = &domain.WorkflowInstance{ID: "missing", Step: "implement", StepEnteredAt: time.Now().UTC()}
	if err := items.Update(context.Background(), wi, raw); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].WorkitemID != "WLM-2" {
		t.Fatalf("started %+v, want the healthy item", report.Started)
	}
	joined := strings.Join(report.Notices, "\n")
	if !strings.Contains(joined, "WLM-1") {
		t.Fatalf("notices = %v, want the broken candidate reported", report.Notices)
	}
}

// A candidate refused for a precondition frees its slot for the next one, and
// when every candidate is refused the tick reports failure instead of success.
func TestDispatchRefusedCandidateFreesItsSlot(t *testing.T) {
	broken := &domain.WorkItem{Title: "坏策略", Priority: 9}
	healthy := &domain.WorkItem{Title: "好策略", Priority: 5}
	root, svc := dispatchFixture(t, "concurrency:\n  global: 1\n", broken, healthy)
	items := workitem.New(root)
	wi, raw, err := items.ReadSnapshot(context.Background(), "WLM-1")
	if err != nil {
		t.Fatal(err)
	}
	wi.Workflow = &domain.WorkflowInstance{ID: "missing", Step: "implement", StepEnteredAt: time.Now().UTC()}
	if err := items.Update(context.Background(), wi, raw); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].WorkitemID != "WLM-2" {
		t.Fatalf("started %+v, want the healthy item to take the refused slot", report.Started)
	}
	if len(report.Notices) == 0 {
		t.Fatalf("report = %+v, want the refusal reported", report)
	}
}

// When nothing can start, the tick says so rather than reporting success.
func TestDispatchAllRefusedIsAFailure(t *testing.T) {
	broken := &domain.WorkItem{Title: "坏策略", Priority: 9}
	root, svc := dispatchFixture(t, "", broken)
	items := workitem.New(root)
	wi, raw, err := items.ReadSnapshot(context.Background(), "WLM-1")
	if err != nil {
		t.Fatal(err)
	}
	wi.Workflow = &domain.WorkflowInstance{ID: "missing", Step: "implement", StepEnteredAt: time.Now().UTC()}
	if err := items.Update(context.Background(), wi, raw); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err == nil {
		t.Fatalf("dispatch reported success after refusing every candidate: %+v", report)
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Class() != KindPrecondition {
		t.Fatalf("error = %v, want a precondition failure", err)
	}
	if len(report.Notices) == 0 {
		t.Fatalf("report = %+v, want the refusal in the notices", report)
	}
}
