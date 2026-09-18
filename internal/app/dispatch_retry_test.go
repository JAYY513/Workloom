package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
)

// setPolicyLimit rewrites the fixture policy's limits block, so the test can
// drive the sweep without declaring a second (invalid) limits key.
func setPolicyLimit(t *testing.T, root, from, to string) {
	t.Helper()
	policyPath := filepath.Join(root, ".devsys", "workflows", "conc.md")
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(policy), from) {
		t.Fatalf("policy does not contain %q:\n%s", from, policy)
	}
	if err := os.WriteFile(policyPath, []byte(strings.Replace(string(policy), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// claimedItem runs one tick so the fixture has a live attempt to sweep.
func claimedItem(t *testing.T, svc *Service, workitemID string) string {
	t.Helper()
	rec := &recorder{}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	for _, attempt := range report.Started {
		if attempt.WorkitemID == workitemID {
			return attempt.RunID
		}
	}
	t.Fatalf("tick did not start %s: %+v", workitemID, report)
	return ""
}

// A failed attempt is requeued with a reproducible backoff, and the retry is
// what the next tick picks up once it is due.
func TestRetrySweepQueuesFailedAttempt(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	// A short ceiling keeps the retry due inside the test.
	setPolicyLimit(t, root, "max_attempts: 3", "max_attempts: 3\n  backoff_max_seconds: 1")
	runID := claimedItem(t, svc, "WLM-1")
	finishRun(t, svc, runID, RunFailed, "the command failed")

	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch after failure: %v", err)
	}
	if len(report.Swept) != 1 {
		t.Fatalf("swept = %+v, want the failed attempt", report.Swept)
	}
	swept := report.Swept[0]
	if swept.Action != "retry_queued" || swept.Reason != "failed" || swept.NextAttemptAt == nil {
		t.Fatalf("swept = %+v, want a queued retry with a due time", swept)
	}
	if !swept.NextAttemptAt.After(svc.now()) {
		t.Fatalf("next attempt %v is not in the future", swept.NextAttemptAt)
	}
	wi := itemState(t, root, "WLM-1")
	if wi.Status != domain.StatusRetryQueued || wi.SchedulingState != domain.SchedulingRetryQueued {
		t.Fatalf("work item = %s/%s, want retry_queued", wi.Status, wi.SchedulingState)
	}
	// Not due yet: the plan reports it as waiting, not as dispatchable.
	if len(report.Plan.Start) != 0 {
		t.Fatalf("plan started %+v while the retry is not due", report.Plan.Start)
	}
	found := false
	for _, decision := range report.Plan.Skip {
		if decision.WorkitemID == "WLM-1" && decision.Reason == "retry_not_due" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan skips = %+v, want retry_not_due", report.Plan.Skip)
	}

	// Once the delay has passed, the retry is a candidate again.
	time.Sleep(1300 * time.Millisecond)
	rec := &recorder{}
	again, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: rec.spawn})
	if err != nil {
		t.Fatalf("dispatch after the delay: %v", err)
	}
	if len(again.Started) != 1 || again.Started[0].WorkitemID != "WLM-1" {
		t.Fatalf("started %+v, want the retry", again.Started)
	}
}

// When the attempts the policy allows are used up, the claim is released and
// the exhaustion is recorded instead of retrying forever.
func TestRetrySweepStopsWhenAttemptsAreExhausted(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	setPolicyLimit(t, root, "max_attempts: 3", "max_attempts: 1")
	runID := claimedItem(t, svc, "WLM-1")
	finishRun(t, svc, runID, RunFailed, "the command failed")

	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Swept) != 1 || report.Swept[0].Action != "released" {
		t.Fatalf("swept = %+v, want the claim released", report.Swept)
	}
	if wi := itemState(t, root, "WLM-1"); wi.SchedulingState != domain.SchedulingUnclaimed {
		t.Fatalf("scheduling state = %q, want the claim gone", wi.SchedulingState)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "retry_exhausted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("retry_exhausted events = %d, want 1", len(events))
	}
}

// An attempt that stopped making progress is ended as stalled and requeued.
func TestRetrySweepEndsStalledAttempt(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	setPolicyLimit(t, root, "max_attempts: 3", "max_attempts: 3\n  stall_threshold_seconds: 60")
	runID := claimedItem(t, svc, "WLM-1")
	// The stream is the progress evidence: an old record means nothing has
	// happened for longer than the policy's threshold.
	stale := time.Now().UTC().Add(-2 * time.Hour)
	streamPath := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	record := `{"t":"` + stale.Format(time.RFC3339Nano) + `","type":"output","stream":"stdout","line":"working"}
`
	if err := os.WriteFile(streamPath, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Swept) != 1 || report.Swept[0].Reason != "stalled" || report.Swept[0].Action != "retry_queued" {
		t.Fatalf("swept = %+v, want the stalled attempt requeued", report.Swept)
	}
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != RunStalled {
		t.Fatalf("run status = %q, want %q (the attempt is over)", view.Run.Status, RunStalled)
	}
	if wi := itemState(t, root, "WLM-1"); wi.Status != domain.StatusRetryQueued {
		t.Fatalf("work item status = %q, want retry_queued", wi.Status)
	}
	// A dry run must not sweep: the sweep writes.
	_, err = svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "preview", DryRun: true, Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if view, err := svc.RunGet(context.Background(), runID); err != nil || view.Run.Status != RunStalled {
		t.Fatalf("dry run changed the run: %v %q", err, view.Run.Status)
	}
}

// A running attempt that is still producing evidence is left alone.
func TestRetrySweepLeavesLiveAttempts(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	setPolicyLimit(t, root, "max_attempts: 3", "max_attempts: 3\n  stall_threshold_seconds: 3600")
	runID := claimedItem(t, svc, "WLM-1")
	streamPath := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	record := `{"t":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","type":"output","stream":"stdout","line":"working"}
`
	if err := os.WriteFile(streamPath, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Swept) != 0 {
		t.Fatalf("swept = %+v, want a live attempt untouched", report.Swept)
	}
	if view, err := svc.RunGet(context.Background(), runID); err != nil || view.Run.Status != "running" {
		t.Fatalf("run status = %q (err %v), want running", view.Run.Status, err)
	}
}

// finishRun records how an attempt ended, the way a failing command does.
func finishRun(t *testing.T, svc *Service, runID, outcome, reason string) {
	t.Helper()
	view, err := svc.RunGet(context.Background(), runID)
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	if _, err := svc.RunFinish(context.Background(), RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: outcome, Actor: "test", Reason: reason,
	}); err != nil {
		t.Fatalf("run finish: %v", err)
	}
}

// A cancellation is a decision, not a failure: the claim is released and no
// attempt is queued.
func TestRetrySweepReleasesCanceledAttempt(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	runID := claimedItem(t, svc, "WLM-1")
	finishRun(t, svc, runID, RunCanceled, "the operator stopped it")

	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Swept) != 1 || report.Swept[0].Action != "released" || report.Swept[0].NextAttemptAt != nil {
		t.Fatalf("swept = %+v, want the canceled attempt released without a retry", report.Swept)
	}
	wi := itemState(t, root, "WLM-1")
	if wi.SchedulingState == domain.SchedulingRetryQueued || wi.NextAttemptAt != nil {
		t.Fatalf("canceled attempt was queued for retry: %+v", wi)
	}
	if wi.SchedulingState != domain.SchedulingUnclaimed {
		t.Fatalf("scheduling state = %q, want the claim released", wi.SchedulingState)
	}
	events, err := svc.EventList(context.Background(), EventListRequest{Type: "retry_exhausted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("cancellation recorded as exhausted retries: %+v", events)
	}
}

// A lease that belongs to a different run than the ended attempt is left
// alone: releasing it would let two agents work the same item. (The foreign run
// exists, so the reconcile step does not treat the lease as an orphan.)
func TestRetrySweepSkipsForeignLease(t *testing.T) {
	root, svc := dispatchFixture(t, "concurrency:\n  global: 2\n",
		&domain.WorkItem{Title: "one", Priority: 9},
		&domain.WorkItem{Title: "two", Priority: 5},
	)
	// One tick with room for both: the two attempts exist side by side.
	report, err := svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	byItem := map[string]string{}
	for _, attempt := range report.Started {
		byItem[attempt.WorkitemID] = attempt.RunID
	}
	first, second := byItem["WLM-1"], byItem["WLM-2"]
	if first == "" || second == "" {
		t.Fatalf("both items must be claimed for the mismatch to be testable: %+v", report.Started)
	}
	// Rewrite WLM-1's lease so it names the other attempt.
	leasePath := filepath.Join(root, ".devsys", "scheduling", "WLM-1.yaml")
	lease, err := os.ReadFile(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(lease), "run_id: "+first, "run_id: "+second, 1)
	if rewritten == string(lease) {
		t.Fatalf("lease does not name the run:\n%s", lease)
	}
	if err := os.WriteFile(leasePath, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	finishRun(t, svc, first, RunFailed, "the command failed")
	report, err = svc.Dispatch(context.Background(), DispatchRequest{Actor: "ops", Reason: "tick", Spawn: (&recorder{}).spawn})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.Swept) != 0 {
		t.Fatalf("swept = %+v, want a foreign lease left alone", report.Swept)
	}
	if wi := itemState(t, root, "WLM-1"); wi.SchedulingState != domain.SchedulingClaimed {
		t.Fatalf("scheduling state = %q, want the claim untouched", wi.SchedulingState)
	}
	if view, err := svc.RunGet(context.Background(), first); err != nil || view.Run.Status != RunFailed {
		t.Fatalf("run status = %q (err %v), want the ended attempt untouched", view.Run.Status, err)
	}
	joined := strings.Join(report.Notices, "\n")
	if !strings.Contains(joined, "belongs to run") {
		t.Fatalf("notices = %v, want the mismatch reported", report.Notices)
	}
}
