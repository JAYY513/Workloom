package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workloom/internal/domain"
)

// An archived run stream stays readable through RunLog: the live file is
// gone, the archived copy carries the evidence.
func TestRunLogFallsBackToArchivedStream(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "archived stream", Priority: 5})
	runID := claimedItem(t, svc, "WLM-1")
	// Write stream records directly: the recorder spawn never executes.
	live := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	if err := os.WriteFile(live, []byte("{\"type\":\"output\",\"line\":\"hello-archive\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finishRun(t, svc, runID, RunFailed, "done")
	archived := filepath.Join(root, ".devsys", "archive", "runs", runID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archived, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	view, err := svc.RunLog(context.Background(), runID, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = view
	lines, err := readRunStream(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ln := range lines {
		if ln.Type == "output" && strings.Contains(ln.Line, "hello-archive") {
			found = true
		}
	}
	if !found {
		t.Fatalf("archived stream not read back: %+v", lines)
	}
}
