package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hookScript renders a hook command for the host shell as a quoted YAML
// scalar: backslashes are escaped because a double-quoted YAML scalar treats
// them as escapes (Windows paths are full of them).
func hookScript(posix, windows string) string {
	command := posix
	if runtime.GOOS == "windows" {
		command = windows
	}
	return "\"" + strings.ReplaceAll(command, "\\", "\\\\") + "\""
}

// hookYAML is one hook declaration for a policy's front matter.
func hookYAML(name, posix, windows string) string {
	return "  " + name + ":\n    command: " + hookScript(posix, windows) + "\n"
}

// hookPolicy is a policy with the lifecycle hooks the workspace tests drive.
func hookPolicy(hooks string) string {
	return `---
id: gated
name: 工作区策略
version: 1
steps:
  - id: implement
    type: execute
    required: true
limits:
  max_attempts: 3
hooks:
` + hooks + `---
实现：{{workitem.title}}
`
}

// prepareAndRun prepares a workspace for the fixture's work item and run.
func prepareAndRun(t *testing.T, workitemID, runID string) (string, string) {
	t.Helper()
	code, out, errOut := run(t, "worktree", "prepare", "--workitem", workitemID, "--run", runID, "--actor", "t", "--reason", "r")
	if code != CodeOK {
		t.Fatalf("worktree prepare: code=%d stderr=%q", code, errOut)
	}
	fields := strings.Fields(out)
	if len(fields) < 4 || fields[3] != "created" {
		t.Fatalf("prepare output = %q, want <key> <path> <branch> created", out)
	}
	return fields[0], fields[1]
}

// Preparing a workspace creates a worktree below the root, binds it to the run
// and records the event; preparing again reuses it without re-running
// after_create (方案 §4.8).
func TestWorktreePrepareCreatesThenReuses(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "after-create")
	hooks := hookYAML("after_create", "echo run >> "+marker, "echo run >> "+marker)
	repo, runID, workitemID := roundProject(t, hookPolicy(hooks))

	key, path := prepareAndRun(t, workitemID, runID)
	if key != "WLM-1" {
		t.Fatalf("key = %q, want WLM-1", key)
	}
	if !strings.HasPrefix(path, filepath.Join(repo, ".devsys", "workspaces")) {
		t.Fatalf("path = %q, want it below .devsys/workspaces", path)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("workspace directory: %v", err)
	}
	if list := gitCmd(t, repo, "worktree", "list", "--porcelain"); !strings.Contains(list, "WLM-1") {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
	// The run now says where it runs.
	code, out, errOut := run(t, "--json", "run", "get", runID)
	if code != CodeOK {
		t.Fatalf("run get: %q", errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ := view["run"].(map[string]any)
	ws, _ := item["workspace"].(map[string]any)
	if ws["path"] != path || ws["branch"] == "" {
		t.Fatalf("run workspace = %v, want the prepared path and branch", ws)
	}
	if count := countLines(t, marker); count != 1 {
		t.Fatalf("after_create ran %d times, want 1", count)
	}

	code, out, errOut = run(t, "worktree", "prepare", "--workitem", workitemID, "--run", runID, "--actor", "t", "--reason", "r")
	if code != CodeOK {
		t.Fatalf("second prepare: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "reused") {
		t.Fatalf("second prepare = %q, want reuse", out)
	}
	if count := countLines(t, marker); count != 1 {
		t.Fatalf("after_create ran again on reuse (%d runs)", count)
	}
	events := readEventTypes(t, repo)
	if events["workspace_created"] == "" || events["workspace_reused"] == "" {
		t.Fatalf("events = %v, want both workspace_created and workspace_reused", events)
	}
}

// A failing after_create aborts preparation and leaves nothing behind.
func TestWorktreePrepareRecoversFromFailingHook(t *testing.T) {
	hooks := hookYAML("after_create", "exit 17", "exit /b 17")
	repo, runID, workitemID := roundProject(t, hookPolicy(hooks))
	code, _, errOut := run(t, "worktree", "prepare", "--workitem", workitemID, "--run", runID, "--actor", "t", "--reason", "r")
	if code != CodePrecondition {
		t.Fatalf("prepare with a failing hook: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "after_create") || !strings.Contains(errOut, "17") {
		t.Fatalf("stderr = %q, want the hook name and its exit code", errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, ".devsys", "workspaces", "WLM-1")); !os.IsNotExist(err) {
		t.Fatalf("half-made workspace left behind: %v", err)
	}
	if list := gitCmd(t, repo, "worktree", "list", "--porcelain"); strings.Contains(list, "WLM-1") {
		t.Fatalf("worktree registration survived:\n%s", list)
	}
}

// Removal needs an explicit force for a dirty workspace, and an already-missing
// workspace is a no-op.
func TestWorktreeRemoveAndList(t *testing.T) {
	repo, runID, workitemID := roundProject(t, hookPolicy(hookYAML("after_create", "true", "exit /b 0")))
	_, path := prepareAndRun(t, workitemID, runID)

	code, out, errOut := run(t, "worktree", "list")
	if code != CodeOK {
		t.Fatalf("worktree list: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "WLM-1") {
		t.Fatalf("list = %q, want the prepared workspace", out)
	}

	if err := os.WriteFile(filepath.Join(path, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "worktree", "remove", "--workitem", workitemID, "--actor", "t", "--reason", "r"); code != CodePrecondition {
		t.Fatalf("removing a dirty workspace: code=%d, want %d", code, CodePrecondition)
	}
	code, out, errOut = run(t, "worktree", "remove", "--workitem", workitemID, "--force", "--actor", "t", "--reason", "r")
	if code != CodeOK {
		t.Fatalf("forced removal: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("remove output = %q", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("workspace still present: %v", err)
	}
	// The branch is kept: it may hold the run's commits.
	if branches := gitCmd(t, repo, "branch", "--list", "devsys/WLM-1"); branches == "" {
		t.Fatal("removal deleted the branch")
	}
	code, out, errOut = run(t, "worktree", "remove", "--workitem", workitemID, "--actor", "t", "--reason", "r")
	if code != CodeOK || !strings.Contains(out, "does not exist") {
		t.Fatalf("second removal: code=%d out=%q stderr=%q, want a no-op", code, out, errOut)
	}
	if events := readEventTypes(t, repo); events["workspace_removed"] == "" {
		t.Fatalf("events = %v, want workspace_removed", events)
	}
}

// A path outside the workspace root is refused: the invariant holds for
// explicit paths too.
func TestWorktreeRejectsPathsOutsideTheRoot(t *testing.T) {
	repo, _, workitemID := roundProject(t, hookPolicy(hookYAML("after_create", "true", "exit /b 0")))
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "worktree", "remove", "--path", outside, "--actor", "t", "--reason", "r")
	if code != CodePrecondition {
		t.Fatalf("removing outside the root: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("the refused path was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".devsys", "workspaces", "..", "..")); err != nil {
		t.Fatalf("project state was disturbed: %v", err)
	}
	_ = workitemID
}

// A bound workspace is where the attempt runs, and a failing before_run hook
// stops the attempt before any agent process starts (§4.8).
func TestRunExecRunsInTheWorkspaceAndHonoursBeforeRun(t *testing.T) {
	repo, runID, workitemID := roundProject(t, hookPolicy(hookYAML("after_create", "true", "exit /b 0")))
	_, path := prepareAndRun(t, workitemID, runID)

	argv := append([]string{"run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--"}, echoArgv("DEVSYS_WORKSPACE")...)
	code, out, errOut := run(t, argv...)
	if code != CodeOK {
		t.Fatalf("run exec in the workspace: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "WLM-1") {
		t.Fatalf("stdout = %q, want the workspace the attempt saw", out)
	}
	if !strings.Contains(out, filepath.Base(path)) {
		t.Fatalf("stdout = %q, want it to name the workspace directory", out)
	}

	// Now a policy whose before_run fails: the attempt must not start.
	broken := hookPolicy(hookYAML("after_create", "true", "exit /b 0") + hookYAML("before_run", "exit 5", "exit /b 5"))
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "workflows", "gated.md"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodePrecondition {
		t.Fatalf("before_run failure: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "before_run") {
		t.Fatalf("stderr = %q, want the hook named", errOut)
	}
	records := readStream(t, repo, runID)
	for _, rec := range records {
		if rec["type"] == "start" && rec["command"] == "git --version" {
			t.Fatalf("the attempt started despite the failing before_run hook: %v", rec)
		}
	}
	if events := readEventTypes(t, repo); events["workspace_hook_failed"] == "" {
		t.Fatalf("events = %v, want workspace_hook_failed", events)
	}
	code, out, errOut = run(t, "--json", "run", "get", runID)
	if code != CodeOK {
		t.Fatalf("run get: %q", errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ := view["run"].(map[string]any)
	if item["status"] != "failed" {
		t.Fatalf("run status = %v, want failed after a fatal before_run hook", item["status"])
	}
}

func countLines(t *testing.T, path string) int {
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
