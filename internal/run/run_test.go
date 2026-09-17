package run

import (
 "context"
 "errors"
 "os"
 "os/exec"
 "path/filepath"
 "reflect"
 "sync"
 "testing"
 "time"

 "workloom/internal/domain"
 "workloom/internal/storage"
)

func newStore(t *testing.T) *Store {
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

func sampleRun(at time.Time) *domain.Run {
 return &domain.Run{
  SchemaVersion: domain.SchemaVersion, ProjectID: "demo",
  WorkItemID: "WLM-1", WorkflowID: "feature-development",
  Agent:   domain.RunAgent{ID: "codex-local", Harness: "codex", Model: "test-model"},
  Status:  "running", Attempt: 1, Phase: "streaming_turns",
  StartedAt: at,
  InputContextRefs: []string{"artifact://project/blueprint"},
 }
}

func TestCreateAssignsDateSequenceIDs(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 at := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
 first, err := s.Create(ctx, sampleRun(at))
 if err != nil { t.Fatal(err) }
 second, err := s.Create(ctx, sampleRun(at))
 if err != nil { t.Fatal(err) }
 if first != "run-20260917-1" || second != "run-20260917-2" {
  t.Fatalf("ids = %s, %s", first, second)
 }
}

func TestConcurrentCreatesUniqueWinners(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 at := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
 const workers = 8
 ids := make([]string, workers)
 var wg sync.WaitGroup
 for i := 0; i < workers; i++ {
  wg.Add(1)
  go func(i int) {
   defer wg.Done()
   id, err := s.Create(ctx, sampleRun(at))
   if err != nil { t.Errorf("worker %d: %v", i, err); return }
   ids[i] = id
  }(i)
 }
 wg.Wait()
 seen := map[string]bool{}
 for _, id := range ids {
  if seen[id] { t.Fatalf("duplicate id %s", id) }
  seen[id] = true
 }
 if len(seen) != workers { t.Fatalf("got %d ids", len(seen)) }
}

func TestGetRestoresContextSnapshot(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 at := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
 r := sampleRun(at)
 r.ContextSnapshot = &domain.ContextSnapshot{
  ProjectStateVersion: 12,
  WorkItemVersion:     4,
  ArtifactVersions:    []string{"artifact-001:v2"},
  KnowledgeRevision:   "20260917-03",
  WorkspaceHead:       "abc1234",
  DecisionIDs:         []string{"decision-001"},
 }
 id, err := s.Create(ctx, r)
 if err != nil { t.Fatal(err) }
 got, err := s.Get(ctx, id)
 if err != nil { t.Fatal(err) }
 if got.ContextSnapshot == nil {
  t.Fatal("context snapshot lost")
 }
 if !reflect.DeepEqual(*got.ContextSnapshot, *r.ContextSnapshot) {
  t.Fatalf("snapshot = %+v, want %+v", *got.ContextSnapshot, *r.ContextSnapshot)
 }
 // run get 还原「Agent 当时看到了什么」: version + revision + decision refs.
 if got.ContextSnapshot.WorkItemVersion != 4 || got.ContextSnapshot.KnowledgeRevision != "20260917-03" {
  t.Fatalf("snapshot view incomplete: %+v", got.ContextSnapshot)
 }
}

func TestListOrderStableAndSorted(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 day1 := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
 day2 := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
 for _, at := range []time.Time{day1, day2} {
  for range 2 {
   if _, err := s.Create(ctx, sampleRun(at)); err != nil { t.Fatal(err) }
  }
 }
 var ids []string
 items, err := s.List(ctx)
 if err != nil { t.Fatal(err) }
 for _, r := range items { ids = append(ids, r.ID) }
 want := []string{"run-20260916-1", "run-20260916-2", "run-20260917-1", "run-20260917-2"}
 if !reflect.DeepEqual(ids, want) { t.Fatalf("ids = %v", ids) }
}

func TestUpdateOptimisticConflict(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 at := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
 id, err := s.Create(ctx, sampleRun(at))
 if err != nil { t.Fatal(err) }
 first, snapA, err := s.ReadSnapshot(ctx, id)
 if err != nil { t.Fatal(err) }
 second, _, err := s.ReadSnapshot(ctx, id)
 if err != nil { t.Fatal(err) }
 first.Status = "finished"
 second.Status = "cancelled"
 if err := s.Update(ctx, first, snapA); err != nil { t.Fatal(err) }
 if err := s.Update(ctx, second, snapA); !errors.Is(err, storage.ErrConflict) {
  t.Fatalf("stale update error = %v", err)
 }
 final, err := s.Get(ctx, id)
 if err != nil { t.Fatal(err) }
 if final.Status != "finished" { t.Fatalf("status = %q", final.Status) }
}

func TestGetRejectsMissingAndInvalidIDs(t *testing.T) {
 ctx := context.Background()
 s := newStore(t)
 if _, err := s.Get(ctx, "run-20260917-9"); err == nil { t.Fatal("expected not found") }
 if _, err := s.Get(ctx, "../escape"); err == nil { t.Fatal("expected invalid id rejection") }
}
