// Package workitem — M3.7 guard contract: a normal transition runs its Guard
// inside the transition transaction; a failing guard rolls the status change
// and its event back.
package workitem

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/storage"
)

func TestTransitionRunsGuardInsideTransaction(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)
	id := createWorkitem(t, s, nil, "guard task")

	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("guard refused")
	err = func() error {
		_, err := s.Transition(ctx, id, TransitionRequest{
			TargetStatus: domain.StatusBacklog, Actor: "test", Reason: "try",
			Guard: func(tx *storage.Tx) ([]*domain.Event, error) {
				// The guard sees the open transaction and the pending state.
				if _, exists, rerr := tx.ReadForExpect(workitemRel(id)); rerr != nil || !exists {
					t.Fatalf("guard could not read the work item: exists=%v err=%v", exists, rerr)
				}
				return nil, sentinel
			},
		}, raw)
		return err
	}()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	unchanged, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != domain.StatusDraft {
		t.Errorf("status = %s, want draft (rolled back)", unchanged.Status)
	}
	evs, err := events.New(root).Read(ctx, events.Filter{Subject: &domain.Reference{Type: "workitem", ID: id}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if strings.Contains(ev.Type, "status_changed") {
			t.Errorf("event leaked despite rollback: %+v", ev)
		}
	}

	// A passing guard commits the transition.
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	updated, err := s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusBacklog, Actor: "test", Reason: "go",
		Guard: func(tx *storage.Tx) ([]*domain.Event, error) {
			ran = true
			return nil, nil
		},
	}, raw)
	if err != nil || !ran || updated.Status != domain.StatusBacklog {
		t.Fatalf("updated = %+v ran=%v err = %v", updated, ran, err)
	}
}
