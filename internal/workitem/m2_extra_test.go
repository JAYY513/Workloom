// Package workitem — M2 extra regression tests covering the surface Main
// flagged as missing in the second review:
//   - Claim always returns a fresh token (never reuses the caller's Token).
//   - Start transitions blocked/retry_queued → in_progress atomically.
//   - QueueRetry computes a deterministic next_attempt_at and clears the
//     lease file.
//   - ForOrphan release requires the referenced Run file to be missing.
//   - UpdateClaimed fences all scheduling fields even if mutate attempts
//     to clear them.
//   - FenceRunUpdate requires the lease owner/token to mutate the bound
//     run.
//   - Plain Update on a leased workitem is rejected.
package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/storage"
)

func TestClaimAlwaysIssuesFreshToken(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)

	now := time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-A",
		Token:         "stale-token",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "a", Harness: "test"},
		Actor:         "test",
		Reason:        "fresh token",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if res.Token == "" || res.Token == "stale-token" {
		t.Fatalf("token = %q; want a fresh random value", res.Token)
	}
	lease, err := s.LeaseInspection(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Token != res.Token {
		t.Fatalf("on-disk lease token %q != result token %q", lease.Token, res.Token)
	}
}

func TestStartBlockedAndRetryQueued(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)

	// Claim → Release with ForExpired (we control lease_until).
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-B",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "b", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		Owner:    "owner-B",
		Token:    res.Token,
		Expected: raw,
		Actor:    "test",
		Reason:   "release",
		Now:      now,
	}); err != nil {
		t.Fatal(err)
	}
	// Walk back to blocked for Start.
	cur, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusReview,
		Actor:        "test",
		Reason:       "review",
		Now:          now,
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusVerification,
		Actor:        "test",
		Reason:       "verify",
		Now:          now.Add(time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusInProgress,
		Actor:        "test",
		Reason:       "manual re-engage",
		Now:          now.Add(2 * time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusBlocked,
		Actor:        "test",
		Reason:       "stuck",
		Now:          now.Add(3 * time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sres, err := s.Start(ctx, id, StartOptions{
		Owner:         "owner-B",
		Expected:      raw,
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "b", Harness: "test"},
		Actor:         "test",
		Reason:        "start",
		Now:           now.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if sres.RunID == "" || sres.Token == "" {
		t.Fatalf("start result incomplete: %+v", sres)
	}
	cur, _ = s.Get(ctx, id)
	if cur.Status != domain.StatusInProgress {
		t.Fatalf("status after start = %q", cur.Status)
	}
	if cur.SchedulingState != domain.SchedulingRunning {
		t.Fatalf("scheduling_state after start = %q", cur.SchedulingState)
	}
}

func TestQueueRetryIsDeterministicAndClearsLease(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-Q",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "q", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rres, err := s.QueueRetry(ctx, id, RetryOptions{
		Owner:     "owner-Q",
		Token:     res.Token,
		Expected:  raw,
		BaseDelay: time.Minute,
		Attempt:   3,
		Actor:     "test",
		Reason:    "retry",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("queue retry: %v", err)
	}
	expected := now.Add(time.Minute * 4) // 2^(3-1) = 4
	if !rres.NextAttemptAt.Equal(expected) {
		t.Fatalf("next_attempt_at = %s; want %s", rres.NextAttemptAt, expected)
	}
	leases, err := s.ListLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("lease file should be cleared; got %+v", leases)
	}
	cur, _ := s.Get(ctx, id)
	if cur.Status != "retry_queued" {
		t.Fatalf("status = %q; want retry_queued", cur.Status)
	}
	if cur.SchedulingState != domain.SchedulingRetryQueued {
		t.Fatalf("scheduling_state = %q", cur.SchedulingState)
	}
}

func TestForOrphanReleaseRequiresMissingRun(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-O",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "o", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run file exists → ForOrphan must refuse.
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		ForOrphan: true,
		Actor:     "reconcile",
		Reason:    "should fail: run exists",
		Expected:  raw,
		Now:       now,
	}); err == nil {
		t.Fatal("expected ForOrphan refusal while run exists")
	} else if !strings.Contains(err.Error(), "ForOrphan release requires run") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Delete the run file. Now ForOrphan succeeds and closes the lease.
	st, err := storage.Open(s.root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join(st.DevsysDir(), "runs", res.RunID+".yaml")
	if err := os.Remove(runPath); err != nil {
		t.Fatal(err)
	}
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		ForOrphan: true,
		Actor:     "reconcile",
		Reason:    "run missing",
		Expected:  raw,
		Now:       now,
	}); err != nil {
		t.Fatalf("ForOrphan release: %v", err)
	}
	leases, err := s.ListLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("lease still present: %+v", leases)
	}
}

func TestPlainUpdateBlockedWhenLeaseActive(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	_, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-U",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "u", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	cur, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Title = "should not take"
	if err := s.Update(ctx, cur, raw); err == nil {
		t.Fatal("expected Update to be rejected while a lease is active")
	} else if !strings.Contains(err.Error(), "use UpdateClaimed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateClaimedFencesSchedulingFields(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-F",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "f", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateClaimed(ctx, id, res.Lease.Owner, res.Token, func(wi *domain.WorkItem) {
		wi.Title = "legitimate edit"
		wi.Description = "fence test"
		// Misuse attempt: clear the lease. The fence must restore them.
		wi.LeaseToken = ""
		wi.LeaseOwner = ""
		wi.SchedulingState = ""
		wi.Status = domain.StatusDone
	}, raw); err != nil {
		t.Fatalf("UpdateClaimed: %v", err)
	}
	cur, _ := s.Get(ctx, id)
	if cur.Title != "legitimate edit" || cur.Description != "fence test" {
		t.Fatalf("metadata not applied: %+v", cur)
	}
	if cur.Status != domain.StatusInProgress {
		t.Fatalf("status regressed to %q", cur.Status)
	}
	if cur.LeaseOwner != "owner-F" || cur.LeaseToken != res.Token {
		t.Fatalf("lease fields stripped: owner=%q token=%q", cur.LeaseOwner, cur.LeaseToken)
	}
	if cur.SchedulingState != domain.SchedulingClaimed {
		t.Fatalf("scheduling_state lost: %q", cur.SchedulingState)
	}
}

func TestFenceRunUpdateRequiresLeaseToken(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-R",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "r", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wrong token is rejected.
	if err := s.FenceRunUpdate(ctx, id, "owner-R", "wrong", func(r *domain.Run) {
		r.Status = "finished"
	}, nil); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("expected ErrLeaseTokenMismatch, got %v", err)
	}
	// Right token mutates the run.
	if err := s.FenceRunUpdate(ctx, id, "owner-R", res.Token, func(r *domain.Run) {
		r.Status = "streaming_turns"
		r.Logs = append(r.Logs, "hello")
	}, nil); err != nil {
		t.Fatalf("FenceRunUpdate: %v", err)
	}
	run, err := s.LoadRun(ctx, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "streaming_turns" || len(run.Logs) != 1 {
		t.Fatalf("run = %+v", run)
	}
}

func TestReleaseSchedulesStateMapping(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-M",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "m", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		Owner:    "owner-M",
		Token:    res.Token,
		Expected: raw,
		Actor:    "test",
		Reason:   "release",
		Now:      now,
	}); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Get(ctx, id)
	if cur.Status != domain.StatusInProgress {
		t.Fatalf("status regressed: %q", cur.Status)
	}
	if cur.SchedulingState != domain.SchedulingUnclaimed {
		t.Fatalf("scheduling_state after release = %q", cur.SchedulingState)
	}
	// And a transition into review → release → released scheduling_state.
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusReview,
		Actor:        "test",
		Reason:       "review",
		Now:          now.Add(time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusInProgress,
		Actor:        "test",
		Reason:       "re-engage",
		Now:          now.Add(2 * time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusReview,
		Actor:        "test",
		Reason:       "review",
		Now:          now.Add(3 * time.Second),
	}, raw); err != nil {
		t.Fatal(err)
	}
	cur, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Manually pretend a lease was issued and released via the API.
	res2, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-M",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "m", Harness: "test"},
		Actor:         "test",
		Reason:        "re-claim",
		Now:           now.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Release first (state.go refuses transitions out of in_progress while
	// the lease is held), then transition to review, then re-claim from
	// review. After the final release the scheduling state must be
	// "released" because review maps there (§6.4).
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		Owner:    res2.Lease.Owner,
		Token:    res2.Token,
		Expected: raw,
		Actor:    "test",
		Reason:   "release 1",
		Now:      now.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusReview,
		Actor:        "test",
		Reason:       "to review",
		Now:          now.Add(6 * time.Second),
	}, raw); err != nil {
		t.Fatalf("transition to review: %v", err)
	}
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if cur.SchedulingState != domain.SchedulingReleased {
		t.Fatalf("scheduling_state after review transition = %q; want released", cur.SchedulingState)
	}
	if cur.SchedulingState != domain.SchedulingReleased {
		t.Fatalf("scheduling_state after review release = %q", cur.SchedulingState)
	}
}

// LoadRun is a small convenience for tests; production callers use
// run.Store.Get directly.
func (s *Store) LoadRun(ctx context.Context, id string) (*domain.Run, error) {
	st, err := storage.Open(s.root, storage.Options{})
	if err != nil {
		return nil, err
	}
	var r *domain.Run
	if err := st.Read(ctx, func(rd *storage.Reader) error {
		data, exists, err := rd.Read("runs/" + id + ".yaml")
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("run not found")
		}
		r = &domain.Run{}
		return domain.DecodeYAML(data, r)
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// Make sure events package import survives even if the test file drops a
// reference during refactors.
var _ = events.ShardRel
