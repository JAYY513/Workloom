// Package archive — M8.3 tests (方案 §14.2).
//
// T1: plan + apply move whole month shards and terminal run streams into
// archive/, with manifest entries and byte accounting; a running run
// refuses the whole batch; re-running is a no-op (all skipped).
// T2: reads merge live and archived state (events.Read covers both trees;
// the run-stream fallback covers archived streams).
// T3: search excludes the archive tree.
package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/search"
	"github.com/JAYY513/Workloom/internal/storage"
)

func seedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	devsys := filepath.Join(root, ".devsys")
	for _, sub := range []string{"events", "runs", "archive", "local"} {
		if err := os.MkdirAll(filepath.Join(devsys, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRun(t *testing.T, root, id, status string) {
	t.Helper()
	rec := &domain.Run{
		SchemaVersion: domain.SchemaVersion,
		ID:            id, ProjectID: "demo", WorkItemID: "WLM-1",
		Status: status, Attempt: 1,
		StartedAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	data, err := domain.EncodeYAML(rec)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".devsys/runs/"+id+".yaml", string(data))
}

func eventLine(t *testing.T, id, tm string) string {
	t.Helper()
	when, err := time.Parse(time.RFC3339, tm)
	if err != nil {
		t.Fatal(err)
	}
	ev := &domain.Event{
		SchemaVersion: domain.SchemaVersion, ID: id, ProjectID: "demo",
		Type: "note", Subject: domain.Reference{Type: "workitem", ID: "WLM-1"},
		Time: when, Actor: "t", Content: id,
	}
	data, err := storage.EncodeJSON(ev)
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

func archiveFixture(t *testing.T) string {
	t.Helper()
	root := seedProject(t)
	writeFile(t, root, ".devsys/events/2026-01.jsonl",
		eventLine(t, "ev-jan-1", "2026-01-10T00:00:00Z")+eventLine(t, "ev-jan-2", "2026-01-20T00:00:00Z"))
	writeFile(t, root, ".devsys/events/2026-02.jsonl",
		eventLine(t, "ev-feb-1", "2026-02-10T00:00:00Z"))
	writeFile(t, root, ".devsys/events/2026-03.jsonl",
		eventLine(t, "ev-mar-1", "2026-03-10T00:00:00Z"))
	writeRun(t, root, "run-20260101-1", "succeeded")
	writeFile(t, root, ".devsys/runs/run-20260101-1.jsonl",
		"{\"type\":\"round\",\"round\":1}\n{\"type\":\"exit\",\"code\":0}\n")
	writeRun(t, root, "run-20260101-2", "running")
	writeFile(t, root, ".devsys/runs/run-20260101-2.jsonl",
		"{\"type\":\"round\",\"round\":1}\n")
	return root
}

// PlanEvents selects whole shards strictly older than the bound.
func TestPlanEventsSelectsOlderShards(t *testing.T) {
	root := archiveFixture(t)
	got, err := PlanEvents(root, "2026-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "events/2026-01.jsonl" || got[1] != "events/2026-02.jsonl" {
		t.Fatalf("plan = %v, want the Jan and Feb shards", got)
	}
	if _, err := PlanEvents(root, "2026-3"); err == nil {
		t.Fatal("non-padded month accepted")
	}
}

// Apply moves shards and streams, writes the manifest, and accounts bytes.
func TestApplyMovesAndManifests(t *testing.T) {
	root := archiveFixture(t)
	ctx := context.Background()
	before, err := events.New(root).Read(ctx, events.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Apply(ctx, root, Spec{
		BeforeMonth: "2026-03", RunIDs: []string{"run-20260101-1"},
		Actor: "ops", Reason: "trim",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Archived) != 3 {
		t.Fatalf("archived = %v, want 2 shards + 1 stream", rep.Archived)
	}
	for _, src := range []string{"events/2026-01.jsonl", "events/2026-02.jsonl", "runs/run-20260101-1.jsonl"} {
		if _, err := os.Stat(filepath.Join(root, ".devsys", filepath.FromSlash(src))); !os.IsNotExist(err) {
			t.Fatalf("live %s still present", src)
		}
	}
	janLive, _ := os.ReadFile(filepath.Join(root, ".devsys", "archive", "events", "2026-01.jsonl"))
	if !strings.Contains(string(janLive), "ev-jan-1") || !strings.Contains(string(janLive), "ev-jan-2") {
		t.Fatal("archived Jan shard lost records")
	}
	if len(rep.Manifest.Entries) != 3 {
		t.Fatalf("manifest entries = %+v, want 3", rep.Manifest.Entries)
	}
	if rep.BytesAfter != rep.BytesBefore-rep.BytesArchived {
		t.Fatalf("bytes %d -> %d (archived %d) do not add up", rep.BytesBefore, rep.BytesAfter, rep.BytesArchived)
	}
	after, err := events.New(root).Read(ctx, events.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("events after = %d, want %d (history stays queryable)", len(after), len(before))
	}
	for i := 1; i < len(after); i++ {
		if after[i].Time.Before(after[i-1].Time) {
			t.Fatal("merged events out of order")
		}
	}
	// Re-running archives nothing new: already-archived sources skip.
	rerun, err := Apply(ctx, root, Spec{
		BeforeMonth: "2026-03", RunIDs: []string{"run-20260101-1"},
		Actor: "ops", Reason: "trim",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rerun.Archived) != 0 || len(rerun.Skipped) == 0 {
		t.Fatalf("rerun = %+v, want all skipped", rerun)
	}
}

// A running run refuses the whole batch: nothing moves.
func TestApplyRefusesRunningRun(t *testing.T) {
	root := archiveFixture(t)
	ctx := context.Background()
	_, err := Apply(ctx, root, Spec{
		RunIDs: []string{"run-20260101-2"}, Actor: "ops", Reason: "trim",
	})
	if err == nil || !strings.Contains(err.Error(), "only terminal runs") {
		t.Fatalf("err = %v, want the terminal-only refusal", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "runs", "run-20260101-2.jsonl")); err != nil {
		t.Fatalf("live stream moved despite refusal: %v", err)
	}
}

// A dry run plans without writing.
func TestDryRunWritesNothing(t *testing.T) {
	root := archiveFixture(t)
	ctx := context.Background()
	rep, err := Apply(ctx, root, Spec{
		BeforeMonth: "2026-03", Actor: "ops", Reason: "trim", DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || len(rep.Archived) != 2 {
		t.Fatalf("dry-run = %+v, want 2 planned moves", rep)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "events", "2026-01.jsonl")); err != nil {
		t.Fatalf("dry run moved live state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "archive", "manifest.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a manifest")
	}
}

// Search never looks inside the archive tree.
func TestSearchExcludesArchive(t *testing.T) {
	root := archiveFixture(t)
	writeFile(t, root, ".devsys/archive/events/1999-01.jsonl", "uniquekeyword-zzz\n")
	matches, _, err := search.Search(filepath.Join(root, ".devsys"), "uniquekeyword-zzz")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("search hit archived content: %+v", matches)
	}
}
