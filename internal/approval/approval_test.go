package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/storage"
)

func newStore(t *testing.T) (string, *Store) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, New(root)
}

func mustRequest(t *testing.T, s *Store, opts RequestOptions) *domain.Approval {
	t.Helper()
	if opts.Scope == "" {
		opts.Scope = ScopeStageGate
	}
	apr, err := s.Request(context.Background(), opts)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return apr
}

func mustApprove(t *testing.T, s *Store, id string) *domain.Approval {
	t.Helper()
	apr, err := s.Decide(context.Background(), id, DecideOptions{Approve: true, DecidedBy: "boss", Comment: "ok"})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	return apr
}

func consume(t *testing.T, root, id, workitemID, stage, status string) error {
	t.Helper()
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st.Write(context.Background(), func(tx *storage.Tx) error {
		ev, err := ConsumeTx(tx, id, workitemID, stage, status, time.Now().UTC(), "gate", "advance")
		if err != nil {
			return err
		}
		return events.AppendTx(tx, ev)
	})
}

func TestRequestLifecycleAndEvents(t *testing.T) {
	root, s := newStore(t)
	ctx := context.Background()

	apr := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "architecture change",
	})
	if apr.ID != "approval-1" || apr.Status != StatusPending || apr.RequestedBy != "requester" {
		t.Fatalf("apr = %+v", apr)
	}

	// Only pending approvals can be decided.
	got := mustApprove(t, s, apr.ID)
	if got.Status != StatusApproved || got.DecidedBy == nil || *got.DecidedBy != "boss" || got.DecidedAt == nil {
		t.Fatalf("approved = %+v", got)
	}
	_, err := s.Decide(ctx, apr.ID, DecideOptions{Approve: false, DecidedBy: "boss"})
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("second decide err = %v", err)
	}

	// Consumption flips consumed_at inside a transaction.
	if err := consume(t, root, apr.ID, "WLM-1", domain.StatusInProgress, domain.StatusReady); err != nil {
		t.Fatalf("consume: %v", err)
	}
	stored, err := s.Get(ctx, apr.ID)
	if err != nil || stored.ConsumedAt == nil {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	// A second consumption is refused.
	if err := consume(t, root, apr.ID, "WLM-1", domain.StatusInProgress, domain.StatusReady); !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("second consume err = %v", err)
	}

	evs, err := events.New(root).Read(ctx, events.Filter{Subject: &domain.Reference{Type: "approval", ID: apr.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, ev := range evs {
		types = append(types, ev.Type)
	}
	want := "approval_requested,approval_approved,approval_consumed"
	if strings.Join(types, ",") != want {
		t.Errorf("events = %v, want %s", types, want)
	}
}

func TestRequestValidation(t *testing.T) {
	_, s := newStore(t)
	ctx := context.Background()
	cases := []RequestOptions{
		{Scope: "bogus", WorkItemID: "WLM-1", RequestedBy: "r"},
		{Scope: ScopeStageGate, WorkItemID: "WLM-1", RequestedBy: "r", Stage: "nope"},
		{Scope: ScopeStageGate, WorkItemID: "", RequestedBy: "r", Stage: domain.StatusReady},
		{Scope: ScopeAction, WorkItemID: "WLM-1", RequestedBy: "r", Stage: domain.StatusReady},
	}
	for _, opts := range cases {
		if _, err := s.Request(ctx, opts); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("opts %+v: err = %v, want ErrInvalidInput", opts, err)
		}
	}
	// action approvals need no stage.
	if _, err := s.Request(ctx, RequestOptions{Scope: ScopeAction, WorkItemID: "WLM-1", RequestedBy: "r", Reason: "danger"}); err != nil {
		t.Fatalf("action request: %v", err)
	}
}

func TestConsumeTxMismatches(t *testing.T) {
	root, s := newStore(t)
	ctx := context.Background()

	approved := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "need",
	})
	mustApprove(t, s, approved.ID)

	if err := consume(t, root, approved.ID, "WLM-2", domain.StatusInProgress, domain.StatusReady); !errors.Is(err, ErrMismatch) {
		t.Errorf("wrong work item err = %v", err)
	}
	if err := consume(t, root, approved.ID, "WLM-1", domain.StatusReview, domain.StatusReady); !errors.Is(err, ErrMismatch) {
		t.Errorf("wrong stage err = %v", err)
	}
	if err := consume(t, root, approved.ID, "WLM-1", domain.StatusInProgress, domain.StatusReview); !errors.Is(err, ErrMismatch) {
		t.Errorf("left status err = %v", err)
	}

	pending := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusVerification, RequestedStatus: domain.StatusReview,
		ProjectID: "demo", RequestedBy: "requester", Reason: "need",
	})
	if err := consume(t, root, pending.ID, "WLM-1", domain.StatusVerification, domain.StatusReview); !errors.Is(err, ErrNotApproved) {
		t.Errorf("pending consume err = %v", err)
	}
	if err := consume(t, root, "approval-99", "WLM-1", domain.StatusVerification, domain.StatusReview); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v", err)
	}

	// The refused transactions left the approval untouched.
	stored, err := s.Get(ctx, approved.ID)
	if err != nil || stored.ConsumedAt != nil {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
}

func TestListAndGetFilters(t *testing.T) {
	_, s := newStore(t)
	ctx := context.Background()
	a1 := mustRequest(t, s, RequestOptions{WorkItemID: "WLM-1", Stage: domain.StatusReady, RequestedStatus: domain.StatusBacklog, RequestedBy: "r", Reason: "x"})
	a2 := mustRequest(t, s, RequestOptions{WorkItemID: "WLM-2", Stage: domain.StatusReview, RequestedStatus: domain.StatusReady, RequestedBy: "r", Reason: "x"})
	mustApprove(t, s, a2.ID)

	all, err := s.List(ctx, Filter{})
	if err != nil || len(all) != 2 || all[0].ID != a1.ID || all[1].ID != a2.ID {
		t.Fatalf("all = %+v err = %v", all, err)
	}
	byWorkitem, err := s.List(ctx, Filter{WorkItemID: "WLM-2"})
	if err != nil || len(byWorkitem) != 1 || byWorkitem[0].ID != a2.ID {
		t.Fatalf("byWorkitem = %+v err = %v", byWorkitem, err)
	}
	pending, err := s.List(ctx, Filter{Status: StatusPending})
	if err != nil || len(pending) != 1 || pending[0].ID != a1.ID {
		t.Fatalf("pending = %+v err = %v", pending, err)
	}

	if _, err := s.Get(ctx, "approval-99"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing get err = %v", err)
	}
	if _, err := s.Get(ctx, "../escape"); !errors.Is(err, ErrBadID) {
		t.Errorf("bad id get err = %v", err)
	}
}

func TestConcurrentDecideOnlyOneWins(t *testing.T) {
	_, s := newStore(t)
	ctx := context.Background()
	apr := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "need",
	})

	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			approve := i%2 == 0
			_, errs[i] = s.Decide(ctx, apr.ID, DecideOptions{Approve: approve, DecidedBy: "boss", Comment: "race"})
		}(i)
	}
	wg.Wait()
	wins := 0
	for i, err := range errs {
		if err == nil {
			wins++
			continue
		}
		if !errors.Is(err, ErrNotPending) {
			t.Fatalf("worker %d: unexpected err %v", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
}

func TestInvalidatedApprovalCannotBeDecidedOrConsumed(t *testing.T) {
	root, s := newStore(t)
	ctx := context.Background()
	apr := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "need",
	})
	mustApprove(t, s, apr.ID)
	pending := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusReview, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "other",
	})

	// The work item leaves the request status: both approvals invalidate.
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(ctx, func(tx *storage.Tx) error {
		evs, ierr := InvalidateForStatusTx(tx, st.DevsysDir(), "WLM-1", domain.StatusReady, time.Now().UTC(), "left ready")
		if ierr != nil {
			return ierr
		}
		return events.AppendBatchTx(tx, evs)
	})
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	if err := consume(t, root, apr.ID, "WLM-1", domain.StatusInProgress, domain.StatusReady); !errors.Is(err, ErrInvalidated) {
		t.Errorf("consume invalidated err = %v", err)
	}
	if _, err := s.Decide(ctx, pending.ID, DecideOptions{Approve: true, DecidedBy: "boss"}); !errors.Is(err, ErrInvalidated) {
		t.Errorf("decide invalidated err = %v", err)
	}

	evs, err := events.New(root).Read(ctx, events.Filter{Type: "approval_invalidated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Errorf("invalidation events = %d, want 2", len(evs))
	}
}

func TestDuplicateOpenRequestRefused(t *testing.T) {
	_, s := newStore(t)
	ctx := context.Background()
	opts := RequestOptions{
		Scope: ScopeStageGate, WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "first",
	}
	first := mustRequest(t, s, opts)
	if _, err := s.Request(ctx, opts); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("duplicate request err = %v", err)
	}
	// A decided (approved) but unconsumed approval still blocks a duplicate.
	mustApprove(t, s, first.ID)
	if _, err := s.Request(ctx, opts); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate after approve err = %v", err)
	}
}

func TestConcurrentRequestsGetUniqueIDs(t *testing.T) {
	_, s := newStore(t)
	ctx := context.Background()
	const workers = 8
	var wg sync.WaitGroup
	ids := make([]string, workers)
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct stages: identical requests are now refused as
			// duplicates, so uniqueness is probed with independent tuples.
			apr, err := s.Request(ctx, RequestOptions{
				Scope: ScopeStageGate, WorkItemID: "WLM-1", Stage: domain.AllStatuses()[i%len(domain.AllStatuses())],
				RequestedBy: "r", Reason: "x",
			})
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = apr.ID
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if id == "" || seen[id] {
			t.Fatalf("duplicate or empty id %q", id)
		}
		seen[id] = true
	}
}

func TestInvalidatedForReturnsTheNewestOfTheStage(t *testing.T) {
	root, s := newStore(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	invalidate := func(wi, status string, when time.Time) {
		t.Helper()
		st, err := storage.Open(root, storage.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Write(ctx, func(tx *storage.Tx) error {
			evs, ierr := InvalidateForStatusTx(tx, st.DevsysDir(), wi, status, when, "left "+status)
			if ierr != nil {
				return ierr
			}
			return events.AppendBatchTx(tx, evs)
		}); err != nil {
			t.Fatalf("invalidate: %v", err)
		}
	}

	first := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "first",
	})
	mustApprove(t, s, first.ID)
	invalidate("WLM-1", domain.StatusReady, at)
	second := mustRequest(t, s, RequestOptions{
		WorkItemID: "WLM-1", Stage: domain.StatusInProgress, RequestedStatus: domain.StatusReady,
		ProjectID: "demo", RequestedBy: "requester", Reason: "second",
	})
	mustApprove(t, s, second.ID)
	invalidate("WLM-1", domain.StatusReady, at.Add(time.Minute))

	got, err := InvalidatedFor(ctx, root, "WLM-1", domain.StatusInProgress)
	if err != nil {
		t.Fatalf("InvalidatedFor: %v", err)
	}
	if got.ID != second.ID {
		t.Errorf("approval = %s, want the newest invalidated one %s", got.ID, second.ID)
	}

	// Another stage, another work item and approvals that are still usable are
	// not answers to this question.
	if _, err := InvalidatedFor(ctx, root, "WLM-1", domain.StatusReview); !errors.Is(err, ErrNotFound) {
		t.Errorf("other stage err = %v, want ErrNotFound", err)
	}
	if _, err := InvalidatedFor(ctx, root, "WLM-2", domain.StatusInProgress); !errors.Is(err, ErrNotFound) {
		t.Errorf("other work item err = %v, want ErrNotFound", err)
	}
}
