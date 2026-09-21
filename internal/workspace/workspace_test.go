package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitFixture is a project repository with one commit: worktree creation needs
// a HEAD to branch from.
func gitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "devsys@example.test")
	git(t, root, "config", "user.name", "devsys test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "fixture")
	return root
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// hookScript renders a hook command for the host shell.
func hookScript(posix, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return posix
}

func worktrees(t *testing.T, root string) string {
	t.Helper()
	return git(t, root, "worktree", "list", "--porcelain")
}

// A workspace is created once and reused afterwards: the after_create hook
// gates on creation and must not run again on reuse (方案 §4.8).
func TestEnsureCreatesOnceThenReuses(t *testing.T) {
	root := gitFixture(t)
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "after-create-count")
	hooks := map[string]Hook{
		HookAfterCreate: {Command: hookScript("echo run >> "+marker, "echo run >> "+marker)},
	}

	first, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-1", Hooks: hooks})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !first.Created {
		t.Fatal("first ensure did not report creation")
	}
	if first.Key != "WLM-1" || !strings.HasPrefix(first.Branch, "devsys/") {
		t.Fatalf("workspace = %+v", first)
	}
	if info, err := os.Stat(first.Path); err != nil || !info.IsDir() {
		t.Fatalf("workspace directory: %v", err)
	}
	if !strings.Contains(worktrees(t, root), filepath.ToSlash(first.Path)) && !strings.Contains(worktrees(t, root), first.Path) {
		t.Fatalf("git does not list the workspace:\n%s", worktrees(t, root))
	}
	if first.Hook == nil || !first.Hook.Ran || first.Hook.ExitCode != 0 {
		t.Fatalf("after_create hook report = %+v", first.Hook)
	}
	before := readCount(t, marker)
	if before != 1 {
		t.Fatalf("after_create ran %d times, want 1", before)
	}

	second, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-1", Hooks: hooks})
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if second.Created || second.Path != first.Path || second.Branch != first.Branch {
		t.Fatalf("reuse = %+v, want the existing workspace", second)
	}
	if got := readCount(t, marker); got != 1 {
		t.Fatalf("after_create ran again on reuse (%d runs)", got)
	}
	if second.Hook != nil {
		t.Fatalf("reuse reported a hook run: %+v", second.Hook)
	}
}

// A failing after_create hook aborts creation and leaves nothing behind: no
// directory, no registration, no branch — and the next attempt can succeed.
func TestEnsureRecoversFromFailingHook(t *testing.T) {
	root := gitFixture(t)
	ctx := context.Background()
	failing := map[string]Hook{
		HookAfterCreate: {Command: hookScript("exit 17", "exit /b 17")},
	}
	_, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-2", Hooks: failing})
	if err == nil {
		t.Fatal("a failing after_create hook did not abort creation")
	}
	var herr *HookError
	if !asHookError(err, &herr) || herr.Name != HookAfterCreate || herr.ExitCode != 17 {
		t.Fatalf("error = %v, want an after_create HookError with exit 17", err)
	}
	path := filepath.Join(root, ".devsys", "workspaces", "WLM-2")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("half-made workspace left behind: %v", err)
	}
	if list := worktrees(t, root); strings.Contains(list, "WLM-2") {
		t.Fatalf("worktree registration survived:\n%s", list)
	}
	if branches := git(t, root, "branch", "--list", "devsys/WLM-2"); branches != "" {
		t.Fatalf("branch survived: %q", branches)
	}

	working := map[string]Hook{
		HookAfterCreate: {Command: hookScript("true", "exit /b 0")},
	}
	retry, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-2", Hooks: working})
	if err != nil {
		t.Fatalf("retry after cleanup: %v", err)
	}
	if !retry.Created {
		t.Fatalf("retry = %+v, want a fresh creation", retry)
	}
}

// A hook that outlives its timeout is fatal the same way.
func TestEnsureHookTimeoutIsFatal(t *testing.T) {
	root := gitFixture(t)
	slow := map[string]Hook{
		HookAfterCreate: {
			Command:        hookScript("sleep 30", "ping -n 30 127.0.0.1 >nul"),
			TimeoutSeconds: 1,
		},
	}
	_, err := Ensure(context.Background(), EnsureOptions{ProjectRoot: root, Identifier: "WLM-3", Hooks: slow})
	var herr *HookError
	if !asHookError(err, &herr) || !herr.TimedOut {
		t.Fatalf("error = %v, want a timed-out HookError", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "workspaces", "WLM-3")); !os.IsNotExist(err) {
		t.Fatalf("workspace survived a timed-out hook: %v", err)
	}
}

// before_remove failure is recorded, never fatal: removal still completes.
func TestRemoveRecordsHookFailureAndStillRemoves(t *testing.T) {
	root := gitFixture(t)
	ctx := context.Background()
	ws, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-4"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	hooks := map[string]Hook{
		HookBeforeRemove: {Command: hookScript("exit 9", "exit /b 9")},
	}
	rep, err := Remove(ctx, RemoveOptions{ProjectRoot: root, Identifier: "WLM-4", Hooks: hooks})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !rep.Removed {
		t.Fatalf("report = %+v, want removal", rep)
	}
	if len(rep.Warnings) == 0 || !strings.Contains(strings.Join(rep.Warnings, " "), HookBeforeRemove) {
		t.Fatalf("warnings = %v, want the before_remove failure", rep.Warnings)
	}
	if rep.Hook == nil || rep.Hook.ExitCode != 9 {
		t.Fatalf("hook report = %+v, want exit 9", rep.Hook)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("workspace still present: %v", err)
	}
	if list := worktrees(t, root); strings.Contains(list, "WLM-4") {
		t.Fatalf("worktree registration survived:\n%s", list)
	}
	// The branch is kept on purpose: it may hold the run's commits.
	if branches := git(t, root, "branch", "--list", "devsys/WLM-4"); branches == "" {
		t.Fatal("removal deleted the branch, which may hold the run's commits")
	}
}

// Uncommitted work needs an explicit --force; an already-missing workspace is a
// no-op, not an error.
func TestRemoveRefusesDirtyWorkspaceWithoutForce(t *testing.T) {
	root := gitFixture(t)
	ctx := context.Background()
	ws, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-5"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "scratch.txt"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(ctx, RemoveOptions{ProjectRoot: root, Identifier: "WLM-5"}); err == nil {
		t.Fatal("a dirty workspace was removed without force")
	} else if !strings.Contains(err.Error(), "force") {
		t.Fatalf("error = %v, want it to mention --force", err)
	}
	rep, err := Remove(ctx, RemoveOptions{ProjectRoot: root, Identifier: "WLM-5", Force: true})
	if err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if !rep.Removed {
		t.Fatalf("report = %+v", rep)
	}
	again, err := Remove(ctx, RemoveOptions{ProjectRoot: root, Identifier: "WLM-5", Force: true})
	if err != nil {
		t.Fatalf("removing a missing workspace: %v", err)
	}
	if again.Removed || again.Notice == "" {
		t.Fatalf("second removal = %+v, want a notice instead of an error", again)
	}
}

// List reports what exists below the root and is read-only.
func TestListReportsRegisteredWorkspaces(t *testing.T) {
	root := gitFixture(t)
	ctx := context.Background()
	for _, id := range []string{"WLM-6", "WLM-7"} {
		if _, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: id}); err != nil {
			t.Fatalf("ensure %s: %v", id, err)
		}
	}
	items, err := List(root, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 || items[0].Key != "WLM-6" || items[1].Key != "WLM-7" {
		t.Fatalf("list = %+v, want both workspaces ordered by key", items)
	}
	for _, item := range items {
		if !item.Registered || item.Branch == "" {
			t.Fatalf("listing = %+v, want a registered worktree with a branch", item)
		}
	}
	if items, err := List(gitFixture(t), ""); err != nil || len(items) != 0 {
		t.Fatalf("empty root: items=%v err=%v", items, err)
	}
}

// A configured root is honoured, and an identifier that needs sanitizing still
// lands inside it.
func TestEnsureUsesConfiguredRoot(t *testing.T) {
	root := gitFixture(t)
	configured := filepath.Join(t.TempDir(), "ws-root")
	ws, err := Ensure(context.Background(), EnsureOptions{
		ProjectRoot: root, Root: configured, Identifier: "feature/login",
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !strings.HasPrefix(ws.Path, configured) {
		t.Fatalf("path = %q, want it below %q", ws.Path, configured)
	}
	if ws.Key != Key("feature/login") || strings.ContainsAny(ws.Key, "/\\") {
		t.Fatalf("key = %q, want a sanitized single segment", ws.Key)
	}
}

func isolatedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if GitProject(dir) {
		t.Skipf("temp dir %s is inside a git repository", dir)
	}
	return dir
}

func TestEnsureDirectoryModeWithoutGit(t *testing.T) {
	root := isolatedDir(t)
	ctx := context.Background()
	first, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-9"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !first.Created || first.Branch != "" {
		t.Fatalf("workspace = %+v, want created directory with no branch", first)
	}
	if info, err := os.Stat(first.Path); err != nil || !info.IsDir() {
		t.Fatalf("directory: %v", err)
	}
	second, err := Ensure(ctx, EnsureOptions{ProjectRoot: root, Identifier: "WLM-9"})
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if second.Created || second.Path != first.Path || second.Branch != "" {
		t.Fatalf("reuse = %+v", second)
	}
	rep, err := Remove(ctx, RemoveOptions{ProjectRoot: root, Identifier: "WLM-9"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !rep.Removed {
		t.Fatalf("remove report = %+v", rep)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("directory still present: %v", err)
	}
}

func readCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(strings.TrimSpace(string(data))))
}

// asHookError unwraps a HookError through the wrapped cleanup messages.
func asHookError(err error, target **HookError) bool {
	for err != nil {
		if he, ok := err.(*HookError); ok {
			*target = he
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
