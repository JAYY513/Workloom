package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStepCompletePrintsExecutableNext(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "flow.md", cliFlowPolicy)
	id := createPlainWorkitem(t, items, "workflow next command")

	code, _, errOut := run(t, "workflow", "start", "--id", id, "--policy", "flow",
		"--actor", "smoke", "--reason", "begin", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("start: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "workflow", "step-complete", "--id", id, "--to", "implement",
		"--actor", "smoke", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("complete: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "command: workloom workflow step-complete --id "+id+" --to verify ") {
		t.Fatalf("stdout = %q, want an executable step-complete for the satisfied edge", out)
	}

	code, _, errOut = run(t, "workflow", "pause", "--id", id, "--actor", "smoke", "--reason", "hold", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("pause: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "step-complete", "--id", id, "--actor", "smoke", "--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "paused") || !strings.Contains(errOut, "workloom workflow resume --id "+id) {
		t.Fatalf("paused complete: code=%d stderr=%q", code, errOut)
	}
	if strings.Contains(errOut, "step-complete --id "+id+" --to ") {
		t.Fatalf("paused refusal must not recommend another step-complete: %s", errOut)
	}
}

func TestSessionStartDoesNotInit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DEVSYS_PROJECT_ROOT", "")
	code, _, errOut := run(t, "session", "start")
	if code == CodeOK {
		t.Fatalf("session start in a fresh directory exited 0")
	}
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%q, want precondition", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".devsys")); !os.IsNotExist(err) {
		t.Fatalf("session start created .devsys: %v", err)
	}
	if !strings.Contains(errOut, "workloom init") {
		t.Fatalf("stderr = %q, want init guidance and no silent init", errOut)
	}
}

func TestQualityGateNamesCurrentWeightThreshold(t *testing.T) {
	_, items := gatedProject(t)
	id := createGatedWorkitem(t, items, "fix", "x")
	code, _, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "dev", "--reason", "start")
	if code != CodeInvalid {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"quality gate not satisfied", "当前", "权重", "阈值", "min_score"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
}
