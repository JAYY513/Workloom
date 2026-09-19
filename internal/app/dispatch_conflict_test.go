// Package app — M8.2 dispatch conflict gate test (方案 §14.4).
//
// An unresolved git merge blocks the whole tick: Dispatch (and its dry-run)
// returns a Precondition error, starts nothing, and creates no runs.
// A probe failure is not a conflict: outside git the tick proceeds.
package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"workloom/internal/domain"
)

func mergeEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
}

func mergeRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = mergeEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustConflict(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = mergeEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git %v: expected a conflict, merge succeeded\n%s", args, out)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// clashFixture leaves an unresolved merge on clash.txt inside a dispatch
// project with one ready work item.
func clashFixture(t *testing.T) (string, *Service) {
	t.Helper()
	root, svc := dispatchFixture(t, "",
		&domain.WorkItem{Title: "blocked by merge", Priority: 5},
	)
	mergeRun(t, root, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeRun(t, root, "add", "-A")
	mergeRun(t, root, "commit", "-q", "-m", "clash base")
	mergeRun(t, root, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeRun(t, root, "add", "-A")
	mergeRun(t, root, "commit", "-q", "-m", "side clash")
	mergeRun(t, root, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeRun(t, root, "add", "-A")
	mergeRun(t, root, "commit", "-q", "-m", "main clash")
	mustConflict(t, root, "merge", "side")
	return root, svc
}

func TestDispatchBlockedByUnresolvedMerge(t *testing.T) {
	root, svc := clashFixture(t)
	rec := &recorder{}
	_, err := svc.Dispatch(context.Background(), DispatchRequest{
		Actor: "ops", Reason: "tick", Spawn: rec.spawn,
	})
	if err == nil || !strings.Contains(err.Error(), "dispatch blocked") {
		t.Fatalf("dispatch err = %v, want the merge-blocked precondition", err)
	}
	if rec.calls != 0 {
		t.Fatalf("spawn calls = %d, want none", rec.calls)
	}
	if ids := runIDsFor(t, root); len(ids) != 0 {
		t.Fatalf("runs created under conflict: %v", ids)
	}
}

func TestDispatchDryRunBlockedByUnresolvedMerge(t *testing.T) {
	_, svc := clashFixture(t)
	rep, err := svc.Dispatch(context.Background(), DispatchRequest{
		Actor: "ops", Reason: "preview", DryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "dispatch blocked") {
		t.Fatalf("dry-run err = %v, want the merge-blocked precondition", err)
	}
	if len(rep.Started) != 0 {
		t.Fatalf("dry-run started = %v, want none", rep.Started)
	}
}

func TestDispatchProceedsWhenProbeDegraded(t *testing.T) {
	// A probe failure is not a conflict: outside git the gate degrades to
	// a notice and lets the tick through. Tested at the gate level — a
	// full tick without .git cannot run for unrelated reasons (workspaces
	// are git worktrees).
	dir := t.TempDir()
	blocked, notice := mergeConflict(dir)
	if blocked != "" {
		t.Fatalf("blocked = %q, want none outside git", blocked)
	}
	if !strings.Contains(notice, "git conflict probe unavailable") {
		t.Fatalf("notice = %q, want the degraded-probe note", notice)
	}
}

func runIDsFor(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".devsys", "runs"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			out = append(out, e.Name())
		}
	}
	return out
}
