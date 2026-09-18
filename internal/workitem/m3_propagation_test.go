// Package workitem — M3.3 completion propagation tests: a completed work
// item's decision/finding records reach its parent and direct siblings as
// references, with events, without touching their status; repeated
// completions do not duplicate anything.
package workitem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/record"
	"workloom/internal/storage"
)

func m3Root(t *testing.T) (string, *Store) {
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
	return root, New(root)
}

func createWorkitem(t *testing.T, s *Store, parent *string, title string) string {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: title,
		Status: domain.StatusDraft, ParentID: parent, CreatedAt: now, UpdatedAt: now,
	}
	id, err := s.Create(ctx, wi, "WLM")
	if err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	return id
}

func walkTo(t *testing.T, s *Store, id string, targets ...string) {
	t.Helper()
	ctx := context.Background()
	for i, target := range targets {
		_, raw, err := s.ReadSnapshot(ctx, id)
		if err != nil {
			t.Fatalf("snapshot %s: %v", id, err)
		}
		if _, err := s.Transition(ctx, id, TransitionRequest{
			TargetStatus: target, Actor: "test", Reason: "fixture",
			Now: time.Date(2026, 9, 18, 2, 0, i+1, 0, time.UTC),
		}, raw); err != nil {
			t.Fatalf("transition %s -> %s: %v", id, target, err)
		}
	}
}

func propagationEvents(t *testing.T, root, workitemID string) []*domain.Event {
	t.Helper()
	evs, err := events.New(root).Read(context.Background(), events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: workitemID},
		Type:    "record_propagated",
	})
	if err != nil {
		t.Fatalf("read events for %s: %v", workitemID, err)
	}
	return evs
}

func TestCompletionPropagatesRecordsToParentAndSiblings(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)

	parentID := createWorkitem(t, s, nil, "parent task")
	siblingID := createWorkitem(t, s, &parentID, "sibling task")
	childID := createWorkitem(t, s, &parentID, "completing task")
	unrelatedID := createWorkitem(t, s, nil, "unrelated task")

	recs := record.New(root)
	if _, err := recs.CreateDecision(ctx, &domain.Decision{
		ProjectID: "demo", Title: "choose text storage", Status: "accepted",
		RelatedWorkItems: []string{childID},
	}); err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if _, err := recs.CreateFinding(ctx, &domain.Finding{
		ProjectID: "demo", Title: "sync needs case care", Type: "risk", Severity: "medium",
		RelatedWorkItems: []string{childID},
	}); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	if _, err := recs.CreateDecision(ctx, &domain.Decision{
		ProjectID: "demo", Title: "unrelated", Status: "accepted",
		RelatedWorkItems: []string{unrelatedID},
	}); err != nil {
		t.Fatalf("create unrelated decision: %v", err)
	}

	walkTo(t, s, childID, domain.StatusBacklog, domain.StatusReady, domain.StatusVerification, domain.StatusDone)

	wantRefs := []string{"decision://decision-1", "finding://finding-1"}
	for _, target := range []string{parentID, siblingID} {
		wi, err := s.Get(ctx, target)
		if err != nil {
			t.Fatalf("get %s: %v", target, err)
		}
		if strings.Join(wi.ContextRefs, ",") != strings.Join(wantRefs, ",") {
			t.Errorf("%s context_refs = %v, want %v", target, wi.ContextRefs, wantRefs)
		}
		if wi.Status != domain.StatusDraft {
			t.Errorf("%s status changed to %s", target, wi.Status)
		}
		if evs := propagationEvents(t, root, target); len(evs) != 1 {
			t.Errorf("%s events = %d, want 1", target, len(evs))
		}
	}

	// The unrelated work item and the completing one stay untouched.
	other, err := s.Get(ctx, unrelatedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.ContextRefs) != 0 {
		t.Errorf("unrelated context_refs = %v", other.ContextRefs)
	}
	if evs := propagationEvents(t, root, unrelatedID); len(evs) != 0 {
		t.Errorf("unrelated events = %d", len(evs))
	}
	if evs := propagationEvents(t, root, childID); len(evs) != 0 {
		t.Errorf("completing work item got its own propagation events: %d", len(evs))
	}

	// A second completion (repair back, then done again) is idempotent.
	_, raw, err := s.ReadSnapshot(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepair(ctx, childID, TransitionRequest{
		TargetStatus: domain.StatusVerification, Actor: "test", Reason: "reopen",
		AllowRepair: true, Confirmation: []byte("test-digest"),
		Guard: func(*storage.Tx) ([]*domain.Event, error) { return nil, nil },
		Now:   time.Date(2026, 9, 18, 2, 1, 0, 0, time.UTC),
	}, raw); err != nil {
		t.Fatalf("repair: %v", err)
	}
	walkTo(t, s, childID, domain.StatusDone)

	for _, target := range []string{parentID, siblingID} {
		wi, err := s.Get(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(wi.ContextRefs, ",") != strings.Join(wantRefs, ",") {
			t.Errorf("%s context_refs after re-completion = %v", target, wi.ContextRefs)
		}
		if evs := propagationEvents(t, root, target); len(evs) != 1 {
			t.Errorf("%s events after re-completion = %d, want 1", target, len(evs))
		}
	}
}

func TestCompletionWithoutParentDoesNotPropagate(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)
	id := createWorkitem(t, s, nil, "lone task")
	if _, err := record.New(root).CreateDecision(ctx, &domain.Decision{
		ProjectID: "demo", Title: "solo", Status: "accepted",
		RelatedWorkItems: []string{id},
	}); err != nil {
		t.Fatal(err)
	}
	walkTo(t, s, id, domain.StatusBacklog, domain.StatusReady, domain.StatusVerification, domain.StatusDone)
	wi, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(wi.ContextRefs) != 0 {
		t.Errorf("context_refs = %v, want none", wi.ContextRefs)
	}
}

// A parent link pointing at the work item itself fails closed with a
// business error and rolls the completion back, instead of tripping the
// storage-level "staged twice" assertion.
func TestCompletionWithSelfParentFailsClosed(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)
	id := createWorkitem(t, s, nil, "self-parented task")
	cur, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.ParentID = &id
	if err := s.Update(ctx, cur, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := record.New(root).CreateDecision(ctx, &domain.Decision{
		ProjectID: "demo", Title: "self", Status: "accepted",
		RelatedWorkItems: []string{id},
	}); err != nil {
		t.Fatal(err)
	}
	walkTo(t, s, id, domain.StatusBacklog, domain.StatusReady, domain.StatusVerification)
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Transition(ctx, id, TransitionRequest{
		TargetStatus: domain.StatusDone, Actor: "test", Reason: "complete",
	}, raw)
	if err == nil || !strings.Contains(err.Error(), "parent link to itself") {
		t.Fatalf("err = %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusVerification {
		t.Errorf("status = %s, want verification (completion rolled back)", got.Status)
	}
}
