package next

import (
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

func TestClassifyAttemptsGraceAndDead(t *testing.T) {
	now := t0
	item := wi("WLM-1", domain.StatusInProgress, 0, now, now)
	item.SchedulingState = domain.SchedulingClaimed
	fresh := &domain.Run{ID: "run-fresh", WorkItemID: "WLM-1", Status: "running", StartedAt: now.Add(-time.Second)}
	dead, inflight := ClassifyAttempts(now, []*domain.WorkItem{item}, map[string]*domain.Run{"WLM-1": fresh}, map[string]int{"run-fresh": 0})
	if len(dead) != 0 || len(inflight) != 1 {
		t.Fatalf("fresh spawn: dead=%+v inflight=%+v, want in_flight", dead, inflight)
	}

	stale := &domain.Run{ID: "run-stale", WorkItemID: "WLM-1", Status: "running", StartedAt: now.Add(-SpawnGrace - time.Second)}
	dead, inflight = ClassifyAttempts(now, []*domain.WorkItem{item}, map[string]*domain.Run{"WLM-1": stale}, map[string]int{"run-stale": 0})
	if len(dead) != 1 || len(inflight) != 0 {
		t.Fatalf("stale empty stream: dead=%+v inflight=%+v, want dead_attempt", dead, inflight)
	}

	failed := &domain.Run{ID: "run-fail", WorkItemID: "WLM-1", Status: "failed", StartedAt: now.Add(-time.Minute)}
	dead, inflight = ClassifyAttempts(now, []*domain.WorkItem{item}, map[string]*domain.Run{"WLM-1": failed}, nil)
	if len(dead) != 1 || dead[0].RunID != "run-fail" || len(inflight) != 0 {
		t.Fatalf("failed run: dead=%+v inflight=%+v", dead, inflight)
	}
}
