package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// syncProject inits a repo with .devsys/ and commits the tracked content.
func syncProject(t *testing.T) string {
	t.Helper()
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%q", code, errOut)
	}
	// Plain add: .devsys/local/ is git-ignored by init's own .gitignore,
	// so it stays out of the commit by convention here.
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "--allow-empty", "-m", "devsys")
	return repo
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Blocked is still exit 0: the handoff state is the answer, not a failure.
// Usage and precondition errors keep their codes.
func TestSyncStatusCodes(t *testing.T) {
	syncProject(t)

	if code, _, _ := run(t, "sync"); code != CodeUsage {
		t.Errorf("sync: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "sync", "bogus"); code != CodeUsage {
		t.Errorf("sync bogus: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "sync", "status", "extra"); code != CodeUsage {
		t.Errorf("sync status extra: code=%d, want %d", code, CodeUsage)
	}
	// No upstream on a fresh repo: blocked verdict, exit 0.
	code, out, errOut := run(t, "sync", "status")
	if code != CodeOK {
		t.Fatalf("status: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "handoff: NOT ready") || !strings.Contains(out, "[no-upstream]") {
		t.Errorf("status output = %q, want the no-upstream block", out)
	}
}

// JSON is the machine contract: ok + handoff_ready + blockers.
func TestSyncStatusJSON(t *testing.T) {
	syncProject(t)

	code, out, errOut := run(t, "--json", "sync", "status")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		OK           bool `json:"ok"`
		HandoffReady bool `json:"handoff_ready"`
		Blockers     []struct {
			Code string `json:"code"`
		} `json:"blockers"`
		Branch string `json:"branch"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if !payload.OK || payload.HandoffReady {
		t.Errorf("payload = %+v, want ok=true ready=false", payload)
	}
	if len(payload.Blockers) != 1 || payload.Blockers[0].Code != "no-upstream" {
		t.Errorf("blockers = %+v, want [no-upstream]", payload.Blockers)
	}
	if payload.Branch == "" || payload.Commit == "" {
		t.Errorf("payload missing branch/commit: %+v", payload)
	}
}

// --quiet prints nothing but still exits 0.
func TestSyncStatusQuiet(t *testing.T) {
	syncProject(t)

	code, out, errOut := run(t, "--quiet", "sync", "status")
	if code != CodeOK || out != "" {
		t.Errorf("quiet: code=%d out=%q stderr=%q", code, out, errOut)
	}
}

// Outside a git repository the command cannot inspect: exit 3, not a verdict.
func TestSyncStatusOutsideRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(dir)
	// A .devsys/ without a git repository around it.
	if err := os.MkdirAll(filepath.Join(dir, ".devsys"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "sync", "status")
	if code != CodePrecondition {
		t.Errorf("outside repo: code=%d, want %d (stderr=%q)", code, CodePrecondition, errOut)
	}
}

// Without .devsys/ the precondition names init: exit 3.
func TestSyncStatusNoDevsys(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "bare")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	code, _, errOut := run(t, "sync", "status")
	if code != CodePrecondition || !strings.Contains(errOut, "devsys init") {
		t.Errorf("no .devsys: code=%d stderr=%q, want 3 with an init hint", code, errOut)
	}
}

// A dirty .devsys/ plus an active lease surface two blocker codes through
// the same JSON wrapper the no-upstream test pins.
func TestSyncStatusJSONBlockers(t *testing.T) {
	repo := syncProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "project.yaml"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "--json", "sync", "status")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		OK           bool `json:"ok"`
		HandoffReady bool `json:"handoff_ready"`
		Blockers     []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if payload.HandoffReady {
		t.Errorf("payload = %+v, want ready=false", payload)
	}
	codes := map[string]bool{}
	for _, b := range payload.Blockers {
		codes[b.Code] = true
	}
	if !codes["no-upstream"] || !codes["uncommitted-devsys"] {
		t.Errorf("blockers = %+v, want no-upstream + uncommitted-devsys", payload.Blockers)
	}
}
