package syncstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
)

func marshal(t *testing.T, st Status) string {
	t.Helper()
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// gitFixture inits a repo with one commit on main.
func gitFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "init", "-q", "-b", "main", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	return root
}

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func seedDevsys(t *testing.T, root string) {
	t.Helper()
	for _, sub := range []string{"local", "workitems", "scheduling", "runs"} {
		if err := os.MkdirAll(filepath.Join(root, ".devsys", sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// snapshotTree records every .devsys byte, like reconcile's no-writes test.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, ".devsys"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(root, ".devsys"), p)
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeLease(t *testing.T, root string, lease *domain.SchedulingLease) {
	t.Helper()
	if lease.SchemaVersion == 0 {
		lease.SchemaVersion = domain.SchemaVersion
	}
	data, err := domain.EncodeYAML(lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devsys", "scheduling", lease.WorkItemID+".yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRun(t *testing.T, root, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".devsys", "runs", id+".yaml"), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasBlocker(st Status, code string) bool {
	for _, b := range st.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

// A fresh repo with no upstream and no .devsys changes is blocked only on
// the missing upstream.
func TestCheckNoUpstream(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	// Tracked .devsys content (workitems/scheduling/runs) must be committed
	// so the tree is clean; .devsys/local/ is git-ignored by convention and
	// stays uncommitted.
	gitRun(t, root, "add", "-A", "--", ".", ":!.devsys/local")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "devsys")

	st, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if st.HandoffReady {
		t.Fatalf("ready with no upstream: %+v", st)
	}
	if !hasBlocker(st, CodeNoUpstream) {
		t.Errorf("missing no-upstream blocker: %+v", st.Blockers)
	}
	if len(st.Blockers) != 1 {
		t.Errorf("blockers = %+v, want only no-upstream", st.Blockers)
	}
}

// Diverged HEAD, unmerged paths, pending transactions and active leases each
// block; the verdict names every one, never just the first.
func TestCheckBlockers(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	remote := filepath.Join(t.TempDir(), "remote")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref: %v\n%s", err, out)
	}
	gitRun(t, root, "remote", "add", "origin", remote)
	// Tracked .devsys content only; local/ stays out of the commit by
	// convention (git-ignored real projects, untracked here).
	gitRun(t, root, "add", "-A", "--", ".", ":!.devsys/local")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "devsys")
	gitRun(t, root, "push", "-q", "-u", "origin", "main")
	// Local-only commit: ahead of upstream.
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "local")

	// Active lease with a referenced run file (uncommitted on purpose: they
	// double as the uncommitted-.devsys evidence the test asserts).
	writeRun(t, root, "run-1")
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1", Owner: "device-a", Token: "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
		RunID:      "run-1",
	})

	// Unmerged conflict on a code path. The side branch commits tracked
	// content only; the lease files stay uncommitted on main.
	gitRun(t, root, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "clash.txt")
	gitRun(t, root, "commit", "-q", "-m", "side")
	gitRun(t, root, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(root, "clash.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "clash.txt")
	gitRun(t, root, "commit", "-q", "-m", "main")
	cmd := exec.Command("git", "-C", root, "merge", "side")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	_ = cmd.Run() // exit 1: conflict expected

	st, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if st.HandoffReady {
		t.Fatalf("ready with blockers present: %+v", st)
	}
	for _, code := range []string{
		CodeDivergedFromUpstream, CodeUnmergedPaths, CodeMergeInProgress,
		CodeActiveLeases, CodeUncommittedDevsys,
	} {
		if !hasBlocker(st, code) {
			t.Errorf("missing %s blocker: %+v", code, st.Blockers)
		}
	}
	if len(st.Unmerged) != 1 || st.Unmerged[0] != "clash.txt" {
		t.Errorf("unmerged = %v, want [clash.txt]", st.Unmerged)
	}
	if len(st.Leases) != 1 || st.Leases[0].WorkitemID != "WLM-1" {
		t.Errorf("leases = %+v, want the WLM-1 lease", st.Leases)
	}
}

// Pending transactions block and defer the lease audit rather than judging
// leases from a partial snapshot.
func TestCheckPendingDefersLeases(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	txnID := "20260101T000000.000000000Z-1-deadbeef"
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local", "txn", txnID, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	jrnl := "txn_id: " + txnID + "\ncreated_at: 2026-01-01T00:00:00Z\npid: 1\nops:\n" +
		"  - kind: put\n    rel: workitems/WLM-1.yaml\n" +
		"    expect: sha256:0000000000000000000000000000000000000000000000000000000000000000\n" +
		"    payload: payload/000.bin\n    payload_sha256: 0000000000000000000000000000000000000000000000000000000000000000\n    payload_size: 0\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "local", "txn", txnID, "journal.yaml"), []byte(jrnl), 0o644); err != nil {
		t.Fatal(err)
	}
	// An active lease exists, but pending material may still apply it.
	writeRun(t, root, "run-1")
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1", Owner: "device-a", Token: "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
		RunID:      "run-1",
	})

	st, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if st.HandoffReady {
		t.Fatalf("ready with pending transactions: %+v", st)
	}
	if !hasBlocker(st, CodePendingTransactions) {
		t.Errorf("missing pending blocker: %+v", st.Blockers)
	}
	if hasBlocker(st, CodeActiveLeases) {
		t.Errorf("lease judged from a partial snapshot: %+v", st.Blockers)
	}
	if !st.LeasesDeferred || len(st.Leases) != 0 {
		t.Errorf("leases must be deferred, got deferred=%v leases=%+v", st.LeasesDeferred, st.Leases)
	}
}

// Expired, orphan and unreadable leases each block with their own code: a
// handoff must not inherit drift the old device never cleaned up.
func TestCheckLeaseKinds(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	gitRun(t, root, "add", "-A", "--", ".", ":!.devsys/local")
	// Expired: lease_until in the past, run file present.
	writeRun(t, root, "run-1")
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1", Owner: "device-a", Token: "tok",
		ClaimedAt:  time.Now().UTC().Add(-2 * time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Hour),
		RunID:      "run-1",
	})
	// Orphan: run file missing.
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-2", Owner: "device-a", Token: "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
		RunID:      "run-gone",
	})
	// Unreadable: not YAML at all.
	if err := os.WriteFile(filepath.Join(root, ".devsys", "scheduling", "WLM-3.yaml"), []byte("{{{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if st.HandoffReady {
		t.Fatalf("ready with lease drift: %+v", st)
	}
	for _, code := range []string{CodeExpiredLeases, CodeOrphanLeases, CodeUnreadableLeases} {
		if !hasBlocker(st, code) {
			t.Errorf("missing %s blocker: %+v", code, st.Blockers)
		}
	}
	kinds := map[string]string{}
	for _, l := range st.Leases {
		kinds[l.WorkitemID] = l.Kind
	}
	if kinds["WLM-1"] != "expired" || kinds["WLM-2"] != "orphan" || kinds["WLM-3"] != "unreadable" {
		t.Errorf("lease kinds = %v, want expired/orphan/unreadable", kinds)
	}
}

// Check writes nothing: the .devsys tree is byte-identical and no lock file
func TestCheckWritesNothing(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	writeRun(t, root, "run-1")
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1", Owner: "device-a", Token: "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
		RunID:      "run-1",
	})
	gitRun(t, root, "add", "-A", "--", ".", ":!.devsys/local")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "devsys")

	before := snapshotTree(t, root)
	if _, err := Check(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	after := snapshotTree(t, root)
	if len(before) != len(after) {
		t.Fatalf("tree changed: %d vs %d files", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("file %s changed", k)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "local", "lock")); !os.IsNotExist(err) {
		t.Fatalf("lock file created by a read-only check")
	}
}

func TestCheckDeterministic(t *testing.T) {
	root := gitFixture(t, map[string]string{"a.txt": "a\n"})
	seedDevsys(t, root)
	gitRun(t, root, "add", "-A", "--", ".", ":!.devsys/local")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "devsys")

	a, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	aj, bj := marshal(t, a), marshal(t, b)
	if aj != bj || !strings.Contains(aj, `"handoff_ready"`) {
		t.Fatalf("checks differ:\n%s\n%s", aj, bj)
	}
}
