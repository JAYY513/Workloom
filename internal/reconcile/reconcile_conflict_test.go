// Package reconcile — M8.2 conflict-marking tests (方案 §14.4).
//
// T1 covers the repair face: an unresolved git merge surfaces as
// human-only `note_unmerged_paths` proposals in the dry-run table, and
// applying the plan writes nothing while routing every note to rejected.
// T3 guards the no-newer-wins invariant: the only LeaseUntil.Before call
// sites in the tree are the expiry checks (reconcile walk, release fencing,
// heartbeat fencing) — a future "newer lease wins" path must fail review,
// and Claim on a held lease stays ErrAlreadyClaimed.
package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func conflictEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
}

func conflictRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = conflictEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func conflictMerge(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = conflictEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git %v: expected a conflict, merge succeeded\n%s", args, out)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// conflictFixture builds a git repo whose .devsys tree has one workitem and
// then leaves an unresolved merge on clash.txt plus MERGE_HEAD behind.
func conflictFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := newProject(t)
	conflictRun(t, root, "init", "-q", "-b", "main")
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "ready", nil))
	conflictRun(t, root, "add", "-A")
	conflictRun(t, root, "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("side-base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflictRun(t, root, "add", "-A")
	conflictRun(t, root, "commit", "-q", "-m", "side base")
	conflictRun(t, root, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflictRun(t, root, "add", "-A")
	conflictRun(t, root, "commit", "-q", "-m", "side")
	conflictRun(t, root, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflictRun(t, root, "add", "-A")
	conflictRun(t, root, "commit", "-q", "-m", "main side")
	conflictMerge(t, root, "merge", "side")
	return root
}

// An unresolved merge appears in the dry-run table as human-only notes with
// a reproducible digest.
func TestRepairDryRunSurfacesUnmergedPaths(t *testing.T) {
	root := conflictFixture(t)
	first, err := RepairDryRun(context.Background(), root, Options{Actor: "t", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var notes []Proposal
	for _, p := range first.Proposals {
		if p.Kind == ProposalNoteUnmerged {
			notes = append(notes, p)
		}
	}
	if len(notes) < 2 {
		t.Fatalf("note proposals = %d, want clash.txt plus MERGE_HEAD", len(notes))
	}
	foundClash, foundMerge := false, false
	for _, p := range notes {
		if strings.Contains(p.Description, "clash.txt") {
			foundClash = true
		}
		if strings.Contains(p.Description, "MERGE_HEAD") {
			foundMerge = true
		}
		if p.Note != "human-only" {
			t.Errorf("note %q missing human-only marker", p.Description)
		}
	}
	if !foundClash || !foundMerge {
		t.Fatalf("notes = %+v, want clash.txt and MERGE_HEAD", notes)
	}
	second, err := RepairDryRun(context.Background(), root, Options{Actor: "t", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest drifted: %s vs %s", first.Digest, second.Digest)
	}
}

// Applying a plan of pure notes writes nothing: every note is rejected with
// a human-resolution message.
func TestRepairApplyRejectsNotesWithoutWriting(t *testing.T) {
	root := conflictFixture(t)
	plan, err := RepairDryRun(context.Background(), root, Options{Actor: "t", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	rep, err := RepairApply(context.Background(), root, plan, Options{Actor: "t", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Applied) != 0 {
		t.Fatalf("applied = %v, want none", rep.Applied)
	}
	noteCount := 0
	for _, p := range plan.Proposals {
		if p.Kind == ProposalNoteUnmerged {
			noteCount++
		}
	}
	if len(rep.Rejected) != noteCount {
		t.Fatalf("rejected = %v, want all %d notes", rep.Rejected, noteCount)
	}
	for _, r := range rep.Rejected {
		if !strings.Contains(r, "requires human resolution") {
			t.Errorf("rejected %q missing human-resolution text", r)
		}
	}
	if after := snapshotTree(t, root); !mapsEqual(before, after) {
		t.Fatal("RepairApply mutated the tree for human-only notes")
	}
}

// Outside git the git probe degrades to a plan note, never to an error:
// most reconcile callers run in non-checkouts.
func TestRepairDryRunOutsideGitKeepsWorking(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "ready", nil))
	plan, err := RepairDryRun(context.Background(), root, Options{Actor: "t", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Note, "git conflict probe unavailable") {
		t.Fatalf("plan note = %q, want the degraded probe note", plan.Note)
	}
	for _, p := range plan.Proposals {
		if p.Kind == ProposalNoteUnmerged {
			t.Fatalf("unexpected note outside git: %+v", p)
		}
	}
}

// Outside git the git probe tested above is the only git touchpoint:
// T3 (no newer-wins) is covered by TestDispatchBlockedByUnresolvedMerge on
// the dispatch face plus the expiry-only LeaseUntil.Before call sites.
