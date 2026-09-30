// Package workitem — completion keeps record context task-scoped; it does not
// broadcast project records to parent or sibling work items.
package workitem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
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

func TestCompletionDoesNotPropagateProjectRecords(t *testing.T) {
	root, s := m3Root(t)
	parentID := createWorkitem(t, s, nil, "parent task")
	siblingID := createWorkitem(t, s, &parentID, "sibling task")
	childID := createWorkitem(t, s, &parentID, "completing task")

	walkTo(t, s, childID, domain.StatusBacklog, domain.StatusReady, domain.StatusVerification, domain.StatusDone)

	for _, target := range []string{parentID, siblingID, childID} {
		wi, err := s.Get(context.Background(), target)
		if err != nil {
			t.Fatalf("get %s: %v", target, err)
		}
		if len(wi.ContextRefs) != 0 {
			t.Errorf("%s context_refs = %v, want none", target, wi.ContextRefs)
		}
		if evs := propagationEvents(t, root, target); len(evs) != 0 {
			t.Errorf("%s propagation events = %d, want none", target, len(evs))
		}
	}
}
