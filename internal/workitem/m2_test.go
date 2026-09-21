// Package workitem — M2.1/M2.2 regression tests. These tests cover the
// contracts Main asked us to lock:
//   - Create rejects statuses outside {draft, backlog, ready, blocked}.
//   - Update refuses status field changes; only Transition/ApplyRepair move
//     status.
//   - Transition rejects illegal moves and reports the allowed list.
//   - Transition accepts the conservative forward graph and the blocked
//     resume edge (when PreviousStatus is supplied).
//   - Concurrent Claim attempts on the same work item see exactly one
//     winner; the loser sees ErrAlreadyClaimed.
//   - Heartbeat / Release against an expired lease reject; ForExpired=true
//     on Release accepts without owner/token.
//   - State + event are written in the same transaction: a torn append
//     would surface as a missing event when the workitem file is updated
//     but the JSONL shard is missing.
package workitem

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/storage"
)

// m2Store returns a Store rooted at a fresh project, with .devsys/ created
// the same way workitem_test.go's helper does. The repository's git
// requirement is satisfied so devsys init runs cleanly.
func m2Store(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

// createReady inserts a work item and walks it forward to "ready" so Claim
// can succeed. Returns the id and the raw bytes used to walk it.
func createReady(t *testing.T, s *Store) (string, []byte) {
	t.Helper()
	ctx := context.Background()
	wi := &domain.WorkItem{
		ProjectID: "demo",
		Type:      "feature",
		Title:     "M2 fixture",
		Status:    domain.StatusDraft,
		CreatedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	}
	id, err := s.Create(ctx, wi, "WLM")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// draft -> backlog -> ready through Transition (no Update bypass).
	for _, target := range []string{domain.StatusBacklog, domain.StatusReady} {
		cur, raw, err := s.ReadSnapshot(ctx, id)
		if err != nil {
			t.Fatalf("read snapshot: %v", err)
		}
		cur.Status = target
		if _, err := s.Transition(ctx, id, TransitionRequest{
			TargetStatus: target,
			Actor:        "test",
			Reason:       "fixture",
			Now:          time.Date(2026, 9, 18, 0, 0, 1, 0, time.UTC),
		}, raw); err != nil {
			t.Fatalf("transition to %s: %v", target, err)
		}
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatalf("final snapshot: %v", err)
	}
	return id, raw
}

func TestCreateRejectsIllegalInitialStatus(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	for _, status := range []string{
		domain.StatusInProgress, domain.StatusReview,
		domain.StatusVerification, domain.StatusDone, domain.StatusCancelled,
	} {
		wi := &domain.WorkItem{
			ProjectID: "demo",
			Status:    status,
			CreatedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
		}
		if _, err := s.Create(ctx, wi, "WLM"); err == nil {
			t.Fatalf("create with status=%s: expected error", status)
		}
	}
}

func TestUpdateRejectsStatusChange(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, raw := createReady(t, s)
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Status = domain.StatusDone // bypass attempt
	if err := s.Update(ctx, cur, raw); err == nil {
		t.Fatalf("update changing status=%s should be rejected", cur.Status)
	} else if !strings.Contains(err.Error(), "status changes must go through") {
		t.Fatalf("update error = %v; want status-bypass guard", err)
	}
}

func TestTransitionRejectsIllegalWithAllowedList(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, raw := createReady(t, s)
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Status = domain.StatusDone // impossible jump
	_, err = s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusDone,
		Actor:        "test",
		Reason:       "should fail",
	}, raw)
	var te *domain.TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("error = %v (%T); want *domain.TransitionError", err, err)
	}
	if te.From != domain.StatusReady || te.To != domain.StatusDone {
		t.Fatalf("transition error from=%q to=%q", te.From, te.To)
	}
	if len(te.Allowed) == 0 {
		t.Fatalf("allowed list empty: %v", te)
	}
}

func TestTransitionFollowsForwardGraphAndResume(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)
	now := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)

	// ready -> in_progress is allowed via Claim; the graph allows it for
	// Transition too, so use Transition here to exercise the conservative
	// forward edge without involving Claim.
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusInProgress,
		Actor:        "test",
		Reason:       "walk",
		Now:          now,
	}, raw); err != nil {
		t.Fatalf("ready -> in_progress: %v", err)
	}

	// in_progress -> blocked records the prior status, then blocked ->
	// previous (resume) requires the PreviousStatus field.
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusBlocked,
		Actor:        "test",
		Reason:       "stuck",
		Now:          now.Add(time.Second),
	}, raw); err != nil {
		t.Fatalf("in_progress -> blocked: %v", err)
	}
	cur, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if cur.PreviousStatus != domain.StatusInProgress {
		t.Fatalf("previous_status = %q, want in_progress", cur.PreviousStatus)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus:   domain.StatusInProgress,
		Actor:          "test",
		Reason:         "resume",
		PreviousStatus: domain.StatusInProgress,
		Now:            now.Add(2 * time.Second),
	}, raw); err != nil {
		t.Fatalf("blocked -> in_progress (resume): %v", err)
	}
}

func TestConcurrentClaimExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)

	const workers = 8
	results := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Claim(ctx, id, ClaimOptions{
				Owner:         "agent-" + string(rune('A'+i)),
				LeaseDuration: time.Minute,
				Agent:         domain.RunAgent{ID: "agent-" + string(rune('A'+i)), Harness: "test"},
				Actor:         "test",
				Reason:        "race",
			})
			results[i] = err
		}(i)
	}
	wg.Wait()

	var winners, losers int
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrAlreadyClaimed):
			losers++
		default:
			t.Logf("unexpected claim error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d; want exactly 1; losers=%d", winners, losers)
	}
	if losers != workers-1 {
		t.Fatalf("losers = %d; want %d", losers, workers-1)
	}

	// The winner's lease file and Run record must be present.
	leases, err := s.ListLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].WorkitemID != id {
		t.Fatalf("leases = %+v; want exactly one for %s", leases, id)
	}
	if _, err := s.LeaseInspection(ctx, id); err != nil {
		t.Fatalf("LeaseInspection after claim: %v", err)
	}
}

func TestHeartbeatExpiredTokenRejected(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)

	now := time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)
	res, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-A",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "a", Harness: "test"},
		Actor:         "test",
		Reason:        "claim",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Wrong token: ErrLeaseTokenMismatch.
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, id, HeartbeatOptions{
		Owner:    "owner-A",
		Token:    "deadbeef",
		Expected: raw,
		Actor:    "test",
		Reason:   "bad token",
		Now:      now.Add(time.Second),
	}); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("bad-token heartbeat = %v; want ErrLeaseTokenMismatch", err)
	}

	// Expired lease (now > lease_until): ErrLeaseExpired.
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, id, HeartbeatOptions{
		Owner:    "owner-A",
		Token:    res.Token,
		Expected: raw,
		Actor:    "test",
		Reason:   "too late",
		Now:      now.Add(2 * time.Minute),
	}); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("expired heartbeat = %v; want ErrLeaseExpired", err)
	}

	// ForExpired release with no lease_until < now error if the lease is
	// still valid. (ForExpired requires verified past-now.)
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		ForExpired: true,
		Actor:      "reconcile",
		Reason:     "should still see live lease",
		Expected:   raw,
		Now:        now.Add(time.Second),
	}); err == nil || !strings.Contains(err.Error(), "lease_until is not past now") {
		t.Fatalf("ForExpired against live lease = %v; want past-now error", err)
	}

	// ForExpired release against the now-expired lease succeeds and
	// clears the lease file without changing workitem status.
	curBefore, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, id, ReleaseOptions{
		ForExpired: true,
		Actor:      "reconcile",
		Reason:     "lease expired",
		Expected:   nil,
		Now:        now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("ForExpired release: %v", err)
	}
	leases, err := s.ListLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("lease file still present after expired release: %+v", leases)
	}
	curAfter, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if curAfter.Status != curBefore.Status {
		t.Fatalf("status regressed: before=%q after=%q", curBefore.Status, curAfter.Status)
	}
}

func TestTransitionAndEventAreInSameTransaction(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)

	now := time.Date(2026, 9, 18, 3, 0, 0, 0, time.UTC)
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusInProgress,
		Actor:        "test",
		Reason:       "atomic check",
		Now:          now,
	}, raw); err != nil {
		t.Fatalf("transition: %v", err)
	}

	// After a successful Transition, both files must reflect the new state:
	// the workitem file's Status, and the events shard must contain a
	// status_changed record for this subject.
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Status != domain.StatusInProgress {
		t.Fatalf("workitem status = %q; want in_progress", cur.Status)
	}

	stream := events.New(s.root)
	subject := domain.Reference{Type: "workitem", ID: id}
	evs, err := stream.Read(ctx, events.Filter{Subject: &subject, Type: "status_changed"})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	found := false
	for _, ev := range evs {
		if strings.Contains(ev.Content, domain.StatusReady+" -> "+domain.StatusInProgress) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("status_changed event missing or wrong content; got %d events", len(evs))
	}
}

func TestClaimAndEventAreInSameTransaction(t *testing.T) {
	ctx := context.Background()
	s := m2Store(t)
	id, _ := createReady(t, s)

	now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	if _, err := s.Claim(ctx, id, ClaimOptions{
		Owner:         "owner-X",
		LeaseDuration: time.Minute,
		Agent:         domain.RunAgent{ID: "x", Harness: "test"},
		Actor:         "test",
		Reason:        "atomic check",
		Now:           now,
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// 1) Lease file must exist.
	leases, err := s.ListLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].WorkitemID != id {
		t.Fatalf("leases = %+v; want exactly one for %s", leases, id)
	}

	// 2) Run record must exist with WorkItemID == id.
	st, err := storage.Open(s.root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var foundRun bool
	if err := st.Read(ctx, func(r *storage.Reader) error {
		entries, err := os.ReadDir(filepath.Join(st.DevsysDir(), "runs"))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, exists, err := r.Read("runs/" + e.Name())
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			var run domain.Run
			if err := domain.DecodeYAML(data, &run); err != nil {
				return err
			}
			if run.WorkItemID == id {
				foundRun = true
				return nil
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan runs: %v", err)
	}
	if !foundRun {
		t.Fatalf("no run record for %s after claim", id)
	}

	// 3) Event shard must contain a claimed event for this subject.
	stream := events.New(s.root)
	subject := domain.Reference{Type: "workitem", ID: id}
	evs, err := stream.Read(ctx, events.Filter{Subject: &subject, Type: "claimed"})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("claimed event count = %d; want 1", len(evs))
	}
}
