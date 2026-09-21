package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/sitestatic"
	"github.com/JAYY513/Workloom/internal/view"
)

// TestWorkspaceBuildStaticWritesSite: the M7.2 happy path lays out the full
// site below --out.
func TestWorkspaceBuildStaticWritesSite(t *testing.T) {
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "build the static site", "offline snapshot")
	gitCommitAll(t, root, "work item")

	out := filepath.Join(t.TempDir(), "site")
	code, stdout, errOut := run(t, "workspace", "build", "--static", "--out", out)
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, errOut)
	}
	if !strings.Contains(stdout, "site:") || !strings.Contains(stdout, "6 pages") {
		t.Errorf("stdout = %q", stdout)
	}
	for _, name := range append([]string{"assets/style.css", "data/model.json"}, sitestatic.PageFiles...) {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tasks.html", "data/model.json", "基线提交"} {
		if !strings.Contains(string(index), want) {
			t.Errorf("index.html missing %q", want)
		}
	}
}

// TestWorkspaceBuildDefaultOut lands under .devsys/dist/site/.
func TestWorkspaceBuildDefaultOut(t *testing.T) {
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "default out", "dist site")
	gitCommitAll(t, root, "work item")

	if code, stdout, errOut := run(t, "workspace", "build", "--static"); code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, errOut)
	}
	for _, name := range append([]string{"assets/style.css", "data/model.json"}, sitestatic.PageFiles...) {
		if _, err := os.Stat(filepath.Join(root, ".devsys", "dist", "site", filepath.FromSlash(name))); err != nil {
			t.Errorf("missing default %s: %v", name, err)
		}
	}
}

// TestWorkspaceBuildJSONEnvelope carries the machine-readable contract.
func TestWorkspaceBuildJSONEnvelope(t *testing.T) {
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "json envelope", "machine output")
	gitCommitAll(t, root, "work item")

	out := filepath.Join(t.TempDir(), "site")
	code, stdout, errOut := run(t, "--json", "workspace", "build", "--static", "--out", out)
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, errOut)
	}
	var got struct {
		OK          bool          `json:"ok"`
		Out         string        `json:"out"`
		Pages       []string      `json:"pages"`
		GeneratedAt string        `json:"generated_at"`
		Baseline    view.Baseline `json:"baseline"`
		TrustState  string        `json:"trust_state"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if !got.OK || len(got.Pages) != 6 || got.TrustState != view.TrustOK {
		t.Errorf("envelope = %+v", got)
	}
	if got.Baseline.Commit == "" {
		t.Errorf("envelope missing baseline commit: %+v", got)
	}
}

// TestWorkspaceBuildWritesNothingOutsideOut: with --out outside the project,
// the command leaves every project file alone (read-only contract).
func TestWorkspaceBuildWritesNothingOutsideOut(t *testing.T) {
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "read-only build", "no writes")
	gitCommitAll(t, root, "work item")

	before := workspaceFingerprint(t, root)
	out := filepath.Join(t.TempDir(), "site")
	if code, _, errOut := run(t, "workspace", "build", "--static", "--out", out); code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if after := workspaceFingerprint(t, root); !equalFingerprints(before, after) {
		t.Fatalf("workspace build changed project files:\n%s", workspaceDiff(before, after))
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "dist")); !os.IsNotExist(err) {
		t.Errorf("default dist/ created despite explicit --out")
	}
}

// TestWorkspaceBuildQuiet suppresses the human line.
func TestWorkspaceBuildQuiet(t *testing.T) {
	_, items := gatedProject(t)
	createGatedWorkitem(t, items, "quiet build", "no output")

	out := filepath.Join(t.TempDir(), "site")
	code, stdout, errOut := run(t, "--quiet", "workspace", "build", "--static", "--out", out)
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if stdout != "" {
		t.Errorf("quiet stdout = %q", stdout)
	}
}

// TestWorkspaceBuildUsageErrors: missing --static, bad flags, illegal --out.
func TestWorkspaceBuildUsageErrors(t *testing.T) {
	gatedProject(t)
	cases := [][]string{
		{"workspace", "build"},
		{"workspace", "build", "--static", "--limit", "0"},
		{"workspace", "build", "--static", "--out", ".devsys/runs"},
		{"workspace", "build", "--static", "--out", ".devsys"},
		{"workspace", "build", "--static", "--out", "."},
		{"workspace", "build", "--static", "extra"},
	}
	for _, args := range cases {
		if code, _, errOut := run(t, args...); code != CodeUsage {
			t.Errorf("%v: code = %d (stderr=%q), want %d", args, code, errOut, CodeUsage)
		}
	}
}

// TestWorkspaceBuildWithoutProject is the precondition failure.
func TestWorkspaceBuildWithoutProject(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(dir)

	code, _, errOut := run(t, "workspace", "build", "--static")
	if code != CodePrecondition {
		t.Fatalf("code = %d, want %d (stderr=%q)", code, CodePrecondition, errOut)
	}
	if !strings.Contains(errOut, "devsys init") {
		t.Errorf("stderr = %q", errOut)
	}
}

func equalFingerprints(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
