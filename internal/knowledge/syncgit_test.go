package knowledge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
}

func mustRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func mustBare(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", "--bare", path).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	// A bare repo's HEAD defaults to refs/heads/master; point it at main so
	// clones check out the pushed branch instead of an unborn HEAD.
	if out, err := exec.Command("git", "--git-dir", path, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref: %v\n%s", err, out)
	}
}

func mustClone(t *testing.T, remote, path string) {
	t.Helper()
	if out, err := exec.Command("git", "clone", "-q", remote, path).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
}

func writePeer(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func peerCommit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "peer"}, {"push", "-q", "origin", "HEAD:main"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = gitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// mustMerge runs a merge that is expected to conflict: exit status 1 with
// conflict output is success; any other failure is fatal.
func mustMerge(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func divergenceFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, _ := gitRepo(t, map[string]string{"a.txt": "base\n"})
	return root
}

// A clean tree with no upstream reports ErrNoUpstream rather than failing.
func TestUpstreamNone(t *testing.T) {
	root := divergenceFixture(t)
	if _, err := Upstream(root); err != ErrNoUpstream {
		t.Fatalf("Upstream err = %v, want ErrNoUpstream", err)
	}
}

// Ahead/behind counts local-only vs upstream-only commits with no fetch.
func TestAheadBehind(t *testing.T) {
	root := divergenceFixture(t)
	remote := filepath.Join(t.TempDir(), "remote")
	mustBare(t, remote)
	mustRun(t, root, "remote", "add", "origin", remote)
	mustRun(t, root, "push", "-q", "-u", "origin", "main")
	mustRun(t, root, "fetch", "-q", "origin")

	if got, err := Upstream(root); err != nil || got != "origin/main" {
		t.Fatalf("Upstream = %q, %v, want origin/main", got, err)
	}

	write(t, root, "local.txt", "local\n")
	mustRun(t, root, "add", "-A")
	mustRun(t, root, "commit", "-q", "-m", "local only")
	ahead, behind, err := AheadBehind(root)
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 1 || behind != 0 {
		t.Fatalf("ahead=%d behind=%d, want 1/0", ahead, behind)
	}
	// A second clone adds an upstream-only commit.
	peer := filepath.Join(t.TempDir(), "peer")
	mustClone(t, remote, peer)
	// The clone may land on an unborn HEAD (bare default vs pushed branch);
	// track the pushed main explicitly before writing the new commit.
	mustRun(t, peer, "checkout", "-q", "-B", "main", "origin/main")
	writePeer(t, peer, "remote.txt", "remote\n")
	peerCommit(t, peer)
	mustRun(t, root, "fetch", "-q", "origin")
	ahead, behind, err = AheadBehind(root)
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 1 || behind != 1 {
		t.Fatalf("ahead=%d behind=%d, want 1/1", ahead, behind)
	}
}

// Porcelain entries separate index/worktree codes and flag unmerged paths;
// rename entries consume the NUL-separated source field.
func TestPorcelainStatus(t *testing.T) {
	root := divergenceFixture(t)

	write(t, root, "mod.txt", "tracked\n")
	mustRun(t, root, "add", "-A")
	mustRun(t, root, "commit", "-q", "-m", "track")
	write(t, root, "mod.txt", "changed\n")
	write(t, root, "new file.txt", "untracked\n")

	entries, err := PorcelainStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]PorcelainEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["mod.txt"]; !ok || e.XY != " M" || e.Unmerged {
		t.Errorf("mod.txt = %+v, want XY=\" M\" unmerged=false", e)
	}
	if e, ok := byPath["new file.txt"]; !ok || e.XY != "??" {
		t.Errorf("new file.txt = %+v, want untracked", e)
	}

	// Rename: the source path follows as a separate NUL field.
	mustRun(t, root, "mv", "mod.txt", "renamed.txt")
	entries, err = PorcelainStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Path == "renamed.txt" && e.Orig == "mod.txt" && e.XY[0] == 'R' {
			found = true
		}
	}
	if !found {
		t.Errorf("rename entry missing or malformed: %+v", entries)
	}

	// Unmerged: both sides add the same path on divergent branches.
	mustRun(t, root, "add", "-A")
	mustRun(t, root, "commit", "-q", "-m", "renamed")
	mustRun(t, root, "checkout", "-q", "-b", "side")
	write(t, root, "clash.txt", "side\n")
	mustRun(t, root, "add", "-A")
	mustRun(t, root, "commit", "-q", "-m", "side")
	mustRun(t, root, "checkout", "-q", "main")
	write(t, root, "clash.txt", "main\n")
	mustRun(t, root, "add", "-A")
	mustRun(t, root, "commit", "-q", "-m", "main")
	mustMerge(t, root, "merge", "side")
	entries, err = PorcelainStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	unmerged := false
	for _, e := range entries {
		if e.Path == "clash.txt" && e.Unmerged {
			unmerged = true
		}
	}
	if !unmerged {
		t.Errorf("clash.txt not flagged unmerged: %+v", entries)
	}
	heads, err := MergeHeads(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) == 0 {
		t.Error("MergeHeads empty during an unresolved merge")
	}
}

// A clean tree has no merge machinery.
func TestMergeHeadsClean(t *testing.T) {
	root := divergenceFixture(t)
	heads, err := MergeHeads(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 0 {
		t.Errorf("heads = %v, want none", heads)
	}
}
