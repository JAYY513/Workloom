package workitem

import (
	"context"
	"errors"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet")
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

func sample(id string) *domain.WorkItem {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	return &domain.WorkItem{
		SchemaVersion: domain.SchemaVersion, ProjectID: "demo", Type: "feature",
		Title: "第一个任务", Status: "draft", Priority: 2,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestCreateAssignsSequentialIDsAndRetriesOnConflict(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first, err := s.Create(ctx, sample(""), "WLM")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(ctx, sample(""), "WLM")
	if err != nil {
		t.Fatal(err)
	}
	if first != "WLM-1" || second != "WLM-2" {
		t.Fatalf("ids = %s, %s", first, second)
	}
}

func TestClaimDraftExplainsReadyTransition(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id, err := s.Create(ctx, sample(""), "WLM")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Claim(ctx, id, ClaimOptions{Owner: "agent", Actor: "agent", Reason: "start"})
	var transition *domain.TransitionError
	if !errors.As(err, &transition) {
		t.Fatalf("error = %v, want transition error", err)
	}
	if !strings.Contains(transition.Reason, "transition this draft work item to ready") {
		t.Fatalf("reason = %q, want draft remediation", transition.Reason)
	}
}

func TestConcurrentCreateSamePrefixExactlyOneWinnerPerID(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	const workers = 8
	ids := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := s.Create(ctx, sample(""), "WLM")
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("id %s created %d times", id, n)
		}
	}
	if len(seen) != workers {
		t.Fatalf("got %d distinct ids", len(seen))
	}
	items, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != workers {
		t.Fatalf("list = %d items", len(items))
	}
}

func TestListOrderIsStableAndSorted(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, prefix := range []string{"A", "B"} {
		for range 3 {
			if _, err := s.Create(ctx, sample(""), prefix); err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 3 {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, wi := range items {
			ids = append(ids, wi.ID)
		}
		want := []string{"A-1", "A-2", "A-3", "B-1", "B-2", "B-3"}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("ids = %v", ids)
		}
	}
}

func TestGetUpdateAndOptimisticConflict(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id, err := s.Create(ctx, sample(""), "WLM")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "第一个任务" {
		t.Fatalf("title = %q", got.Title)
	}

	// Two readers see the same bytes; the second writer must lose.
	first, snapA, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	second, snapB, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	first.Title = "并发修改 A"
	second.Title = "并发修改 B"
	if err := s.Update(ctx, first, snapA); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, second, snapB); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	final, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Title != "并发修改 A" {
		t.Fatalf("title = %q", final.Title)
	}
}

func TestGetMissingAndInvalidIDs(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.Get(ctx, "WLM-9"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := s.Get(ctx, "../escape"); err == nil {
		t.Fatal("expected invalid id rejection")
	}
}

func TestSequenceContinuesAcrossProcesses(t *testing.T) {
	// The number must come from the directory, not from memory: a fresh
	// Store handle must still pick N+1, and a lowercase prefix normalizes
	// to the same sequence space.
	ctx := context.Background()
	s := newStore(t)
	for i := 1; i <= 3; i++ {
		if _, err := s.Create(ctx, sample(""), "WLM"); err != nil {
			t.Fatal(err)
		}
	}
	fresh := New(s.root)
	id, err := fresh.Create(ctx, sample(""), "WLM")
	if err != nil {
		t.Fatal(err)
	}
	if id != "WLM-4" {
		t.Fatalf("id = %s", id)
	}
	other, err := fresh.Create(ctx, sample(""), "wlm")
	if err != nil {
		t.Fatal(err)
	}
	if other != "WLM-5" {
		t.Fatalf("prefix case handling: %s", other)
	}
}
