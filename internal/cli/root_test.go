package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// An attempt runs inside its workspace, a git worktree that carries its own
// copy of .devsys/: DEVSYS_PROJECT_ROOT points devsys at the project's real
// state so an agent's reports do not land in the copy.
func TestProjectRootOverride(t *testing.T) {
	repo := dispatchProject(t, "", "one")
	t.Setenv("DEVSYS_PROJECT_ROOT", repo)
	// Run from somewhere else entirely: the override must win.
	t.Chdir(t.TempDir())
	code, out, errOut := run(t, "--json", "project", "get")
	if code != CodeOK {
		t.Fatalf("project get with DEVSYS_PROJECT_ROOT: code=%d stderr=%q", code, errOut)
	}
	if out == "" {
		t.Fatal("no output")
	}
	// Without the override the same directory has no project.
	if err := os.Unsetenv("DEVSYS_PROJECT_ROOT"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "project", "get"); code != CodePrecondition {
		t.Fatalf("project get without the override: code=%d, want %d", code, CodePrecondition)
	}
	// A path that is not a project still fails cleanly.
	t.Setenv("DEVSYS_PROJECT_ROOT", filepath.Join(t.TempDir(), "nowhere"))
	if code, _, errOut := run(t, "project", "get"); code != CodePrecondition {
		t.Fatalf("project get with a bogus root: code=%d stderr=%q", code, errOut)
	}
}
